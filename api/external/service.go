package external

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tempmail/store"
)

//go:embed schema.sql
var schema string

type Service struct {
	db       *pgxpool.Pool
	settings *store.Store
	cipher   cipher.AEAD
}
type Credentials struct {
	Password     string `json:"password"`
	ClientID     string `json:"client_id"`
	RefreshToken string `json:"refresh_token"`
}
type Account struct {
	ID          string      `json:"id"`
	Address     string      `json:"address"`
	Provider    string      `json:"provider"`
	Host        string      `json:"host"`
	Port        int         `json:"port"`
	Protocol    string      `json:"protocol"`
	Authority   string      `json:"authority"`
	Enabled     bool        `json:"enabled"`
	TG          bool        `json:"tg_enabled"`
	LastSync    *time.Time  `json:"last_sync"`
	LastError   string      `json:"last_error"`
	Syncing     bool        `json:"syncing"`
	Credentials Credentials `json:"credentials"`
	Since       time.Time   `json:"-"`
}
type Mailbox struct {
	ID        string `json:"id"`
	AccountID string `json:"account_id"`
	Address   string `json:"address"`
	TG        bool   `json:"tg_enabled"`
	Count     int    `json:"count"`
}
type Share struct {
	MailboxID string     `json:"mailbox_id"`
	Address   string     `json:"address"`
	APIKey    string     `json:"api_key"`
	Enabled   bool       `json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func New(ctx context.Context, dsn, keyFile string, settings *store.Store) (*Service, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*Service, error) { db.Close(); return nil, e }
	key, err := os.ReadFile(keyFile)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(filepath.Dir(keyFile), 0700); err != nil {
			return fail(err)
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return fail(err)
		}
		f, e := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return fail(e)
		}
		_, err = f.Write(key)
		closeErr := f.Close()
		if err != nil {
			return fail(err)
		}
		if closeErr != nil {
			return fail(closeErr)
		}
	} else if err != nil {
		return fail(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fail(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fail(err)
	}
	if _, err = db.Exec(ctx, schema); err != nil {
		return fail(err)
	}
	return &Service{db: db, settings: settings, cipher: aead}, nil
}
func (s *Service) Close() { s.db.Close() }
func (s *Service) seal(id string, c Credentials) ([]byte, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	n := make([]byte, s.cipher.NonceSize())
	if _, err = rand.Read(n); err != nil {
		return nil, err
	}
	return s.cipher.Seal(n, n, data, []byte(id)), nil
}
func (s *Service) unseal(id string, b []byte) (Credentials, error) {
	var c Credentials
	n := s.cipher.NonceSize()
	if len(b) < n {
		return c, errors.New("invalid encrypted credentials")
	}
	data, err := s.cipher.Open(nil, b[:n], b[n:], []byte(id))
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(data, &c)
	return c, err
}
func address(v string) string {
	a, e := mail.ParseAddress(strings.TrimSpace(v))
	if e != nil {
		return ""
	}
	return strings.ToLower(a.Address)
}
func normalize(a *Account) error {
	a.Address = address(a.Address)
	if a.Address == "" {
		return errors.New("请输入有效邮箱地址")
	}
	hosts := map[string]string{"gmail": "imap.gmail.com", "outlook": "outlook.office365.com", "qq": "imap.qq.com", "163": "imap.163.com", "126": "imap.126.com", "yahoo": "imap.mail.yahoo.com", "aliyun": "imap.aliyun.com", "aliyun_enterprise": "imap.qiye.aliyun.com", "custom": ""}
	h, ok := hosts[a.Provider]
	if !ok {
		return errors.New("不支持的邮箱类型")
	}
	if a.Host == "" {
		a.Host = h
	}
	if a.Port == 0 {
		a.Port = 993
	}
	if a.Host == "" || a.Port < 1 || a.Port > 65535 {
		return errors.New("请填写 IMAP 服务器和有效端口")
	}
	if a.Provider != "outlook" {
		a.Protocol = "imap"
	} else {
		if a.Protocol == "" {
			a.Protocol = "auto"
		}
		if a.Protocol != "auto" && a.Protocol != "graph" && a.Protocol != "imap_oauth" {
			return errors.New("无效的 Outlook 接入协议")
		}
		if a.Authority == "" {
			a.Authority = "common"
		}
		if a.Authority != "common" && a.Authority != "consumers" {
			return errors.New("无效的 OAuth 账号类型")
		}
	}
	return nil
}
func (s *Service) accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.Query(ctx, `SELECT id,config,enabled,tg_enabled,last_sync,last_error,COALESCE(lease_until>NOW(),false),initial_since FROM external_accounts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		var data []byte
		var id string
		var enabled, tg, busy bool
		var last *time.Time
		var since time.Time
		var msg string
		if err = rows.Scan(&id, &data, &enabled, &tg, &last, &msg, &busy, &since); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &a); err != nil {
			return nil, err
		}
		a.ID = id
		a.Enabled = enabled
		a.TG = tg
		a.LastSync = last
		a.LastError = msg
		a.Syncing = busy
		a.Since = since
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Service) account(ctx context.Context, id string) (Account, error) {
	var a Account
	var data, b []byte
	var enabled, tg bool
	var last *time.Time
	var since time.Time
	err := s.db.QueryRow(ctx, `SELECT config,credentials,enabled,tg_enabled,last_sync,initial_since FROM external_accounts WHERE id=$1`, id).Scan(&data, &b, &enabled, &tg, &last, &since)
	if err != nil {
		return a, err
	}
	if err = json.Unmarshal(data, &a); err != nil {
		return a, err
	}
	a.ID = id
	a.Enabled = enabled
	a.TG = tg
	a.LastSync = last
	a.Since = since
	a.Credentials, err = s.unseal(id, b)
	return a, err
}
func (s *Service) save(ctx context.Context, a Account) (Account, error) {
	if err := normalize(&a); err != nil {
		return a, err
	}
	if a.ID == "" {
		a.ID = uuid.NewString()
	} else {
		old, err := s.account(ctx, a.ID)
		if err != nil {
			return a, err
		}
		if old.Address != a.Address || old.Provider != a.Provider || old.Host != a.Host || old.Port != a.Port {
			return a, errors.New("接入账号身份不可更换，请新增账号；原分享始终绑定原账号")
		}
		if a.Credentials.Password == "" {
			a.Credentials.Password = old.Credentials.Password
		}
		if a.Credentials.ClientID == "" {
			a.Credentials.ClientID = old.Credentials.ClientID
		}
		if a.Credentials.RefreshToken == "" {
			a.Credentials.RefreshToken = old.Credentials.RefreshToken
		}
	}
	if a.Provider == "outlook" {
		if a.Credentials.ClientID == "" || a.Credentials.RefreshToken == "" {
			return a, errors.New("需要 Client ID 和 Refresh Token")
		}
	} else if a.Credentials.Password == "" {
		return a, errors.New("需要授权码或应用专用密码")
	}
	encrypted, err := s.seal(a.ID, a.Credentials)
	if err != nil {
		return a, err
	}
	a.Credentials = Credentials{}
	data, _ := json.Marshal(a)
	_, err = s.db.Exec(ctx, `INSERT INTO external_accounts(id,config,credentials,enabled) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET config=$2,credentials=$3,enabled=$4,requested=true`, a.ID, string(data), encrypted, a.Enabled)
	return a, err
}
func (s *Service) mailboxes(ctx context.Context, id string) ([]Mailbox, error) {
	rows, err := s.db.Query(ctx, `SELECT b.id,b.account_id,b.address,b.tg_enabled,(SELECT count(*) FROM external_messages m WHERE m.account_id=b.account_id AND m.recipient=b.address AND m.active) FROM external_mailboxes b WHERE b.account_id=$1 ORDER BY b.address`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mailbox{}
	for rows.Next() {
		var b Mailbox
		if err = rows.Scan(&b.ID, &b.AccountID, &b.Address, &b.TG, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Service) mailbox(ctx context.Context, id string) (Mailbox, error) {
	var b Mailbox
	err := s.db.QueryRow(ctx, `SELECT id,account_id,address,tg_enabled FROM external_mailboxes WHERE id=$1`, id).Scan(&b.ID, &b.AccountID, &b.Address, &b.TG)
	return b, err
}
func (s *Service) ensureMailbox(ctx context.Context, accountID, recipient string) error {
	if recipient == "" {
		return nil
	}
	_, err := s.db.Exec(ctx, `INSERT INTO external_mailboxes(id,account_id,address) VALUES($1,$2,$3) ON CONFLICT(account_id,address) DO NOTHING`, uuid.NewString(), accountID, recipient)
	return err
}
