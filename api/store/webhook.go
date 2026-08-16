package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"tempmail/model"
	"tempmail/webhook"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) InsertEmailWithWebhook(ctx context.Context, mailboxID uuid.UUID, recipient, sender, subject, bodyText, bodyHTML, raw string, enqueue bool) (*model.Email, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var email model.Email
	err = tx.QueryRow(ctx,
		`INSERT INTO emails (mailbox_id, sender, subject, body_text, body_html, raw_message, size_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, mailbox_id, sender, subject, body_text, body_html, raw_message, size_bytes, received_at`,
		mailboxID, sender, subject, bodyText, bodyHTML, raw, len(raw),
	).Scan(&email.ID, &email.MailboxID, &email.Sender, &email.Subject, &email.BodyText, &email.BodyHTML, &email.RawMessage, &email.SizeBytes, &email.ReceivedAt)
	if err != nil {
		return nil, err
	}
	if enqueue {
		payload, marshalErr := json.Marshal(map[string]any{
			"id":         email.ID.String(),
			"to":         recipient,
			"from":       email.Sender,
			"subject":    email.Subject,
			"raw":        email.RawMessage,
			"parsedText": email.BodyText,
			"parsedHtml": email.BodyHTML,
			"receivedAt": email.ReceivedAt.UTC().Format(time.RFC3339Nano),
		})
		if marshalErr != nil {
			return nil, marshalErr
		}
		if _, err = tx.Exec(ctx,
			`INSERT INTO webhook_outbox (email_id, payload) VALUES ($1, $2) ON CONFLICT (email_id) DO NOTHING`,
			email.ID, string(payload),
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &email, nil
}

func (s *Store) NextWebhookDelivery(ctx context.Context) (*webhook.Delivery, error) {
	var delivery webhook.Delivery
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, payload, attempts
		 FROM webhook_outbox
		 WHERE next_attempt_at <= NOW()
		 ORDER BY next_attempt_at, created_at
		 LIMIT 1`,
	).Scan(&delivery.ID, &delivery.Payload, &delivery.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &delivery, nil
}

func (s *Store) CompleteWebhookDelivery(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM webhook_outbox WHERE id = $1`, id)
	return err
}

func (s *Store) RetryWebhookDelivery(ctx context.Context, id string, attempts int, next time.Time, lastError string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webhook_outbox
		 SET attempts = $2, next_attempt_at = $3, last_error = LEFT($4, 1000)
		 WHERE id = $1`,
		id, attempts, next, lastError,
	)
	return err
}

func (s *Store) DeleteExpiredWebhookDeliveries(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM webhook_outbox WHERE created_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
