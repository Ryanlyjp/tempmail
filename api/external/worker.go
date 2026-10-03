package external

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tempmail/mailutil"
	"tempmail/model"
	"tempmail/telegrambot"
)

func (s *Service) Run(ctx context.Context) {
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); s.runWorker(ctx) }()
	}
	workers.Wait()
}
func (s *Service) runWorker(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for ctx.Err() == nil && s.cycle(ctx) {
			}
		}
	}
}
func (s *Service) cycle(ctx context.Context) bool {
	var id string
	err := s.db.QueryRow(ctx, `UPDATE external_accounts SET lease_until=NOW()+INTERVAL '11 minutes',requested=false WHERE id=(SELECT id FROM external_accounts WHERE enabled AND (lease_until IS NULL OR lease_until<NOW()) AND (requested OR last_sync IS NULL OR last_sync<NOW()-INTERVAL '60 seconds') ORDER BY last_sync NULLS FIRST LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING id`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		log.Printf("[external] acquire: %v", err)
		return false
	}
	work, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	a, err := s.account(work, id)
	if err == nil {
		err = s.sync(work, a)
	}
	msg := ""
	if err != nil {
		msg = err.Error()
		log.Printf("[external] sync %s: %s", id, msg)
	}
	_, finishErr := s.db.Exec(ctx, `UPDATE external_accounts SET last_error=$2,lease_until=NULL,last_sync=NOW() WHERE id=$1`, id, msg)
	if finishErr != nil {
		log.Printf("[external] finish %s: %v", id, finishErr)
	}
	return true
}
func (s *Service) sync(ctx context.Context, a Account) error {
	run := uuid.NewString()
	// Suppress historical notifications until a complete successful import.
	var first bool
	if err := s.db.QueryRow(ctx, `SELECT imported_at IS NULL FROM external_accounts WHERE id=$1`, a.ID).Scan(&first); err != nil {
		return err
	}
	var err error
	if a.Provider == "outlook" && a.Protocol != "imap_oauth" {
		err = s.graphSync(ctx, a, run, first)
		if err != nil && a.Protocol == "auto" {
			err = s.imapSync(ctx, a, run, first, false)
		}
	} else {
		err = s.imapSync(ctx, a, run, first, false)
	}
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `UPDATE external_messages SET active=false WHERE account_id=$1 AND seen_run<>$2`, a.ID, run)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec(ctx, `UPDATE external_accounts SET imported_at=COALESCE(imported_at,NOW()) WHERE id=$1`, a.ID); err != nil {
		return err
	}
	return s.notify(ctx, a)
}
func (s *Service) known(ctx context.Context, a Account, ref, run string, promotion bool) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE external_messages SET seen_run=$3,active=true,promotion=$4 WHERE id=(SELECT message_id FROM external_sources WHERE account_id=$1 AND source_ref=$2)`, a.ID, ref, run, promotion)
	return tag.RowsAffected() > 0, err
}
func (s *Service) ingest(ctx context.Context, a Account, ref, stable, run string, raw []byte, received time.Time, promotion, first bool) error {
	if received.Before(a.Since) {
		return nil
	}
	e, err := parseEmail(raw, received)
	if err != nil {
		return fmt.Errorf("解析邮件失败: %w", err)
	}
	recipient := originalRecipient(raw, a.Address)
	if stable == "" {
		sum := sha256.Sum256(raw)
		stable = "raw:" + hex.EncodeToString(sum[:])
	}
	e.ID = uuid.New()
	e.Recipient = recipient
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO external_messages(id,account_id,source_key,recipient,message,raw,received_at,promotion,notified,seen_run) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(account_id,source_key) DO UPDATE SET seen_run=$10,active=true,promotion=$8 RETURNING id`, e.ID, a.ID, stable, recipient, string(data), raw, received, promotion, first, run).Scan(&id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO external_sources(account_id,source_ref,message_id) VALUES($1,$2,$3) ON CONFLICT(account_id,source_ref) DO UPDATE SET message_id=$3`, a.ID, ref, id)
	if err != nil {
		return err
	}
	if recipient != "" {
		_, err = tx.Exec(ctx, `INSERT INTO external_mailboxes(id,account_id,address) VALUES($1,$2,$3) ON CONFLICT(account_id,address) DO NOTHING`, uuid.NewString(), a.ID, recipient)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Service) notify(ctx context.Context, a Account) error {
	rows, err := s.db.Query(ctx, `SELECT m.id,m.message,m.raw,m.recipient,(a.tg_enabled OR COALESCE(b.tg_enabled,false)),m.promotion FROM external_messages m JOIN external_accounts a ON a.id=m.account_id LEFT JOIN external_mailboxes b ON b.account_id=m.account_id AND b.address=m.recipient WHERE m.account_id=$1 AND m.active AND NOT m.notified ORDER BY m.received_at`, a.ID)
	if err != nil {
		return err
	}
	type pending struct {
		id        string
		e         model.Email
		raw       []byte
		recipient string
		tg, promo bool
	}
	items := []pending{}
	for rows.Next() {
		var p pending
		var data []byte
		if err = rows.Scan(&p.id, &data, &p.raw, &p.recipient, &p.tg, &p.promo); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(data, &p.e); err != nil {
			rows.Close()
			return err
		}
		items = append(items, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range items {
		if p.tg && !p.promo {
			cfg, err := telegrambot.LoadConfig(ctx, s.settings)
			if err != nil {
				return err
			}
			if !telegrambot.ConfigReady(cfg) {
				return errors.New("TG 已启用，但系统 Bot 配置不完整")
			}
			p.e.RawMessage = string(p.raw)
			assets, err := mailutil.ParseAttachments(p.e.RawMessage)
			if err != nil {
				return err
			}
			target := p.recipient
			if target == "" {
				target = a.Address
			}
			if err = telegrambot.SendEmail(ctx, cfg, model.Mailbox{FullAddress: target}, p.e, assets); err != nil {
				return errors.New("TG 提醒发送失败，将在下次同步重试")
			}
		}
		if _, err = s.db.Exec(ctx, `UPDATE external_messages SET notified=true WHERE id=$1`, p.id); err != nil {
			return err
		}
	}
	return nil
}
