package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"tempmail/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

var ErrMailboxOTPShareTokenConflict = errors.New("mailbox otp share token already exists")

type DomainFilter struct {
	Status   string
	Hostname string
	Query    string
}

const domainSelectColumns = `
	SELECT d.id, d.domain, COALESCE(h.hostname, d.hostname) AS hostname, d.hostname_id,
	       d.is_active, d.status, d.subdomain_enabled, d.subdomain_random_length,
	       d.created_at, d.mx_checked_at
	FROM domains d
	LEFT JOIN hostnames h ON h.id = d.hostname_id
`

// New 创建带连接池的 Store（高并发核心）
func New(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	// 连接池：不限并发，由 PgBouncer 统一管控实际 PG 连接数
	cfg.MaxConns = 500
	cfg.MinConns = 20
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	// PgBouncer transaction 模式不支持 named prepared statements。
	// pgx v5 默认使用 QueryExecModeCacheStatement（会发送 Parse/Bind/Execute），
	// 多个连接复用同一个后端连接时会触发 "prepared statement already in use"。
	// 改为 SimpleProtocol：直接发送明文 SQL，完全绕过服务端 prepared statement 机制。
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	s := &Store{pool: pool}
	if err := s.ensureSchemaCompat(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) ensureSchemaCompat(ctx context.Context) error {
	stmts := []string{
		`ALTER TABLE mailboxes
			ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '30 minutes'`,
		`CREATE INDEX IF NOT EXISTS idx_mailboxes_expires_at ON mailboxes (expires_at)`,
		`ALTER TABLE domains
			ADD COLUMN IF NOT EXISTS status VARCHAR(16) NOT NULL DEFAULT 'active'`,
		`ALTER TABLE domains
			ADD COLUMN IF NOT EXISTS hostname VARCHAR(255) NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS hostnames (
			id SERIAL PRIMARY KEY,
			hostname VARCHAR(255) NOT NULL UNIQUE,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hostnames_active
			ON hostnames (is_active) WHERE is_active = TRUE`,
		`ALTER TABLE domains
			ADD COLUMN IF NOT EXISTS hostname_id INT`,
		`ALTER TABLE domains
			DROP CONSTRAINT IF EXISTS domains_hostname_id_fkey`,
		`ALTER TABLE domains
			ADD CONSTRAINT domains_hostname_id_fkey
			FOREIGN KEY (hostname_id) REFERENCES hostnames(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_domains_hostname_id
			ON domains (hostname_id)`,
		`UPDATE domains
			SET status = CASE WHEN is_active THEN 'active' ELSE 'disabled' END
			WHERE status <> 'pending'`,
		`ALTER TABLE domains
			ADD COLUMN IF NOT EXISTS mx_checked_at TIMESTAMPTZ`,
		`ALTER TABLE domains
			ADD COLUMN IF NOT EXISTS subdomain_enabled BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE domains
			ADD COLUMN IF NOT EXISTS subdomain_random_length INT NOT NULL DEFAULT 5`,
		`ALTER TABLE domains
			DROP CONSTRAINT IF EXISTS domains_subdomain_random_length_chk`,
		`ALTER TABLE domains
			ADD CONSTRAINT domains_subdomain_random_length_chk
			CHECK (subdomain_random_length BETWEEN 2 AND 8)`,
		`CREATE INDEX IF NOT EXISTS idx_domains_subdomain_enabled
			ON domains (subdomain_enabled) WHERE subdomain_enabled = TRUE`,
		`CREATE INDEX IF NOT EXISTS idx_domains_status
			ON domains (status) WHERE status = 'pending'`,
		`ALTER TABLE mailboxes
			ADD COLUMN IF NOT EXISTS is_favorite BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE mailboxes
			ADD COLUMN IF NOT EXISTS tg_forward_enabled BOOLEAN NOT NULL DEFAULT FALSE`,
		`CREATE INDEX IF NOT EXISTS idx_mailboxes_favorite
			ON mailboxes (is_favorite) WHERE is_favorite = TRUE`,
		`CREATE TABLE IF NOT EXISTS favorite_groups (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name VARCHAR(64) NOT NULL,
			sort_order INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_favorite_groups_account_name
			ON favorite_groups (account_id, LOWER(name))`,
		`CREATE INDEX IF NOT EXISTS idx_favorite_groups_account_sort
			ON favorite_groups (account_id, sort_order, created_at)`,
		`ALTER TABLE mailboxes
			ADD COLUMN IF NOT EXISTS favorite_group_id UUID`,
		`ALTER TABLE mailboxes
			DROP CONSTRAINT IF EXISTS mailboxes_favorite_group_id_fkey`,
		`ALTER TABLE mailboxes
			ADD CONSTRAINT mailboxes_favorite_group_id_fkey
			FOREIGN KEY (favorite_group_id) REFERENCES favorite_groups(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_mailboxes_favorite_group
			ON mailboxes (account_id, favorite_group_id, created_at DESC)
			WHERE is_favorite = TRUE`,
		`CREATE TABLE IF NOT EXISTS favorite_group_preferences (
			account_id UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
			group_id UUID REFERENCES favorite_groups(id) ON DELETE SET NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`INSERT INTO favorite_groups (account_id, name, sort_order)
		 SELECT a.id, '收藏夹1', 0
		 FROM accounts a
		 WHERE NOT EXISTS (
			SELECT 1 FROM favorite_groups g WHERE g.account_id = a.id
		 )`,
		`UPDATE mailboxes m
		 SET favorite_group_id = g.id
		 FROM favorite_groups g
		 WHERE m.account_id = g.account_id
		   AND m.is_favorite = TRUE
		   AND m.favorite_group_id IS NULL
		   AND g.id = (
			SELECT g2.id FROM favorite_groups g2
			WHERE g2.account_id = m.account_id
			ORDER BY g2.sort_order, g2.created_at, g2.id
			LIMIT 1
		   )`,
		`INSERT INTO favorite_group_preferences (account_id, group_id)
		 SELECT a.id, (
			SELECT g.id FROM favorite_groups g
			WHERE g.account_id = a.id
			ORDER BY g.sort_order, g.created_at, g.id
			LIMIT 1
		 )
		 FROM accounts a
		 ON CONFLICT (account_id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS mailbox_otp_shares (
			mailbox_id UUID PRIMARY KEY REFERENCES mailboxes(id) ON DELETE CASCADE,
			token VARCHAR(96) NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mailbox_otp_shares_token
			ON mailbox_otp_shares (token)`,
		`INSERT INTO app_settings (key, value) VALUES ('smtp_server_ip', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('smtp_hostname', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('mailbox_ttl_minutes', '30')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('mailbox_page_size', '6')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('otp_segmented_enabled', 'false')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('otp_segmented_lengths', '3')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('otp_segmented_senders', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('site_title', 'TempMail')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('default_domain', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('announcement', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('max_mailboxes_per_user', '100')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('catchall_enabled', 'false')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('catchall_account_id', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('cf_api_token', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('api_subdomain_enabled', 'false')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('api_subdomain_length', '5')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('api_domain_strategy', 'random')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('api_domain_fixed', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('api_mailbox_ttl_minutes', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('tg_bot_token', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('tg_chat_id', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('tg_message_thread_id', '')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO app_settings (key, value) VALUES ('tg_forward_mode', 'all_with_attachments')
			ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO hostnames (hostname)
		 SELECT LOWER(TRIM(value))
		 FROM app_settings
		 WHERE key = 'smtp_hostname' AND TRIM(value) <> ''
		 ON CONFLICT (hostname) DO NOTHING`,
		`INSERT INTO hostnames (hostname)
		 SELECT DISTINCT LOWER(TRIM(hostname))
		 FROM domains
		 WHERE TRIM(hostname) <> ''
		 ON CONFLICT (hostname) DO NOTHING`,
		`UPDATE domains d
		 SET hostname_id = h.id,
		     hostname = h.hostname
		 FROM hostnames h
		 WHERE TRIM(d.hostname) <> ''
		   AND LOWER(TRIM(d.hostname)) = h.hostname
		   AND (d.hostname_id IS NULL OR d.hostname <> h.hostname)`,
	}

	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ensure schema compatibility: %w", err)
		}
	}

	return nil
}

// ==================== Account ====================

func (s *Store) GetAccountByAPIKey(ctx context.Context, apiKey string) (*model.Account, error) {
	var a model.Account
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, api_key, is_admin, is_active, created_at, updated_at
		 FROM accounts WHERE api_key = $1 AND is_active = TRUE`, apiKey,
	).Scan(&a.ID, &a.Username, &a.APIKey, &a.IsAdmin, &a.IsActive, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) CreateAccount(ctx context.Context, username string) (*model.Account, error) {
	apiKey := generateAPIKey()
	var a model.Account
	err := s.pool.QueryRow(ctx,
		`INSERT INTO accounts (username, api_key) VALUES ($1, $2)
		 RETURNING id, username, api_key, is_admin, is_active, created_at, updated_at`,
		username, apiKey,
	).Scan(&a.ID, &a.Username, &a.APIKey, &a.IsAdmin, &a.IsActive, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) DeleteAccount(ctx context.Context, accountID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID)
	return err
}

func (s *Store) ListAccounts(ctx context.Context, page, size int) ([]model.Account, int, error) {
	var total int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, username, api_key, is_admin, is_active, created_at, updated_at
		 FROM accounts ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		size, (page-1)*size,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	accounts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[model.Account])
	if err != nil {
		return nil, 0, err
	}
	return accounts, total, nil
}

// GetAdminAPIKey 获取第一个管理员账号的 API Key（用于写入 admin.key 文件）
func (s *Store) GetAdminAPIKey(ctx context.Context) (string, error) {
	var apiKey string
	err := s.pool.QueryRow(ctx,
		`SELECT api_key FROM accounts WHERE is_admin = TRUE ORDER BY created_at LIMIT 1`,
	).Scan(&apiKey)
	return apiKey, err
}

// ==================== Domain ====================

func (s *Store) AddDomain(ctx context.Context, domain, hostname string) (*model.Domain, error) {
	return s.AddDomainWithOptions(ctx, domain, nil, hostname, false, 5)
}

// AddDomainWithOptions 添加（已激活）域名，并可指定多级子域名相关字段
func (s *Store) AddDomainWithOptions(ctx context.Context, domain string, hostnameID *int, hostname string, subdomainEnabled bool, subdomainRandomLength int) (*model.Domain, error) {
	if subdomainRandomLength < 2 || subdomainRandomLength > 8 {
		subdomainRandomLength = 5
	}
	normalizedHostname := strings.ToLower(strings.TrimSpace(hostname))
	var hostnameRef any
	if hostnameID != nil && *hostnameID > 0 {
		hostnameRef = *hostnameID
	}
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		`INSERT INTO domains (domain, hostname, hostname_id, is_active, status, subdomain_enabled, subdomain_random_length)
		 VALUES ($1, $2, $3, TRUE, 'active', $4, $5)
		 RETURNING id, domain, hostname, hostname_id, is_active, status,
		           subdomain_enabled, subdomain_random_length, created_at, mx_checked_at`,
		strings.ToLower(domain), normalizedHostname, hostnameRef, subdomainEnabled, subdomainRandomLength,
	).Scan(&d.ID, &d.Domain, &d.Hostname, &d.HostnameID, &d.IsActive, &d.Status,
		&d.SubdomainEnabled, &d.SubdomainRandomLength, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// AddDomainPending 添加待验证域名（后台轮询 MX 记录）
func (s *Store) AddDomainPending(ctx context.Context, domain, hostname string) (*model.Domain, error) {
	return s.AddDomainPendingWithOptions(ctx, domain, nil, hostname, false, 5)
}

// AddDomainPendingWithOptions 添加待验证域名，并可指定多级子域名相关字段
func (s *Store) AddDomainPendingWithOptions(ctx context.Context, domain string, hostnameID *int, hostname string, subdomainEnabled bool, subdomainRandomLength int) (*model.Domain, error) {
	if subdomainRandomLength < 2 || subdomainRandomLength > 8 {
		subdomainRandomLength = 5
	}
	normalizedHostname := strings.ToLower(strings.TrimSpace(hostname))
	var hostnameRef any
	if hostnameID != nil && *hostnameID > 0 {
		hostnameRef = *hostnameID
	}
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		`INSERT INTO domains (domain, hostname, hostname_id, is_active, status, subdomain_enabled, subdomain_random_length)
		 VALUES ($1, $2, $3, FALSE, 'pending', $4, $5)
		 ON CONFLICT (domain) DO UPDATE
		   SET status = CASE WHEN domains.status = 'active' THEN 'active' ELSE 'pending' END,
		       is_active = CASE WHEN domains.status = 'active' THEN TRUE ELSE FALSE END,
		       hostname = CASE WHEN EXCLUDED.hostname <> '' THEN EXCLUDED.hostname ELSE domains.hostname END,
		       hostname_id = CASE WHEN EXCLUDED.hostname_id IS NOT NULL THEN EXCLUDED.hostname_id ELSE domains.hostname_id END,
		       subdomain_enabled = EXCLUDED.subdomain_enabled OR domains.subdomain_enabled,
		       subdomain_random_length = EXCLUDED.subdomain_random_length
		 RETURNING id, domain, hostname, hostname_id, is_active, status,
		           subdomain_enabled, subdomain_random_length, created_at, mx_checked_at`,
		strings.ToLower(domain), normalizedHostname, hostnameRef, subdomainEnabled, subdomainRandomLength,
	).Scan(&d.ID, &d.Domain, &d.Hostname, &d.HostnameID, &d.IsActive, &d.Status,
		&d.SubdomainEnabled, &d.SubdomainRandomLength, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) ListDomains(ctx context.Context) ([]model.Domain, error) {
	rows, err := s.pool.Query(ctx, domainSelectColumns+` ORDER BY d.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Domain])
}

func (s *Store) ListDomainsFiltered(ctx context.Context, filter DomainFilter) ([]model.Domain, error) {
	where := []string{"TRUE"}
	args := []any{}
	argPos := 1

	if filter.Status != "" {
		where = append(where, fmt.Sprintf("d.status = $%d", argPos))
		args = append(args, filter.Status)
		argPos++
	}
	if filter.Hostname != "" {
		where = append(where, fmt.Sprintf("COALESCE(h.hostname, d.hostname) = $%d", argPos))
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Hostname)))
		argPos++
	}
	if filter.Query != "" {
		where = append(where, fmt.Sprintf("d.domain ILIKE $%d", argPos))
		args = append(args, "%"+strings.TrimSpace(filter.Query)+"%")
		argPos++
	}

	query := domainSelectColumns + fmt.Sprintf(" WHERE %s ORDER BY d.created_at", strings.Join(where, " AND "))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Domain])
}

func (s *Store) GetDomainSummary(ctx context.Context) (*model.DomainSummary, error) {
	var summary model.DomainSummary
	err := s.pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) AS total,
		  COUNT(*) FILTER (WHERE status = 'active') AS active,
		  COUNT(*) FILTER (WHERE status = 'pending') AS pending,
		  COUNT(*) FILTER (WHERE status = 'disabled') AS disabled
		FROM domains`,
	).Scan(&summary.Total, &summary.Active, &summary.Pending, &summary.Disabled)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func (s *Store) GetActiveDomains(ctx context.Context) ([]model.Domain, error) {
	rows, err := s.pool.Query(ctx, domainSelectColumns+` WHERE d.is_active = TRUE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Domain])
}

// ListSubdomainEnabledDomains 返回所有开启了多级子域的活跃域名（创建邮箱等场景用）
func (s *Store) ListSubdomainEnabledDomains(ctx context.Context) ([]model.Domain, error) {
	rows, err := s.pool.Query(ctx, domainSelectColumns+` WHERE d.is_active = TRUE AND d.subdomain_enabled = TRUE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Domain])
}

func (s *Store) GetRandomActiveDomain(ctx context.Context) (*model.Domain, error) {
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		domainSelectColumns+` WHERE d.is_active = TRUE ORDER BY RANDOM() LIMIT 1`,
	).Scan(&d.ID, &d.Domain, &d.Hostname, &d.HostnameID, &d.IsActive, &d.Status,
		&d.SubdomainEnabled, &d.SubdomainRandomLength, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetDomainByName 按域名字符串查找活跃域名，供创建邮箱时指定域名使用
func (s *Store) GetDomainByName(ctx context.Context, domain string) (*model.Domain, error) {
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		domainSelectColumns+` WHERE d.domain = $1 AND d.is_active = TRUE`,
		strings.ToLower(domain),
	).Scan(&d.ID, &d.Domain, &d.Hostname, &d.HostnameID, &d.IsActive, &d.Status,
		&d.SubdomainEnabled, &d.SubdomainRandomLength, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetHostedDomainByName 按域名字符串查找已录入域名（不要求 active），供 SMTP 收件 / catch-all 使用。
func (s *Store) GetHostedDomainByName(ctx context.Context, domain string) (*model.Domain, error) {
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		domainSelectColumns+` WHERE d.domain = $1`,
		strings.ToLower(domain),
	).Scan(&d.ID, &d.Domain, &d.Hostname, &d.HostnameID, &d.IsActive, &d.Status,
		&d.SubdomainEnabled, &d.SubdomainRandomLength, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListHostedSubdomainEnabledDomains 返回所有开启了多级子域的已录入域名（不要求 active），供 SMTP 收件后缀匹配使用。
func (s *Store) ListHostedSubdomainEnabledDomains(ctx context.Context) ([]model.Domain, error) {
	rows, err := s.pool.Query(ctx, domainSelectColumns+` WHERE d.subdomain_enabled = TRUE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Domain])
}

func (s *Store) GetDomainByID(ctx context.Context, domainID int) (*model.Domain, error) {
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		domainSelectColumns+` WHERE d.id = $1`,
		domainID,
	).Scan(&d.ID, &d.Domain, &d.Hostname, &d.HostnameID, &d.IsActive, &d.Status,
		&d.SubdomainEnabled, &d.SubdomainRandomLength, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListPendingDomains 返回所有待验证域名
func (s *Store) ListPendingDomains(ctx context.Context) ([]model.Domain, error) {
	rows, err := s.pool.Query(ctx, domainSelectColumns+` WHERE d.status = 'pending' ORDER BY d.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Domain])
}

// PromoteDomainToActive 验证通过，激活域名
func (s *Store) PromoteDomainToActive(ctx context.Context, domainID int) error {
	now := time.Now()
	_, err := s.pool.Exec(ctx,
		`UPDATE domains SET is_active = TRUE, status = 'active', mx_checked_at = $1 WHERE id = $2`,
		now, domainID)
	return err
}

// TouchDomainCheckTime 更新最后检测时间
func (s *Store) TouchDomainCheckTime(ctx context.Context, domainID int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE domains SET mx_checked_at = NOW() WHERE id = $1`, domainID)
	return err
}

// DisableDomainMX MX检测失败，自动停用域名
func (s *Store) DisableDomainMX(ctx context.Context, domainID int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE domains SET is_active = FALSE, status = 'disabled', mx_checked_at = NOW() WHERE id = $1`,
		domainID)
	return err
}

func (s *Store) DeleteDomain(ctx context.Context, domainID int) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM domains WHERE id = $1`, domainID)
	return err
}

func (s *Store) UpdateDomainHostname(ctx context.Context, domainID int, hostnameID *int, hostname string) error {
	var hostnameRef any
	if hostnameID != nil && *hostnameID > 0 {
		hostnameRef = *hostnameID
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE domains SET hostname = $1, hostname_id = $2 WHERE id = $3`,
		strings.ToLower(strings.TrimSpace(hostname)), hostnameRef, domainID,
	)
	return err
}

func (s *Store) ToggleDomain(ctx context.Context, domainID int, active bool) error {
	status := "disabled"
	if active {
		status = "active"
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE domains SET is_active = $1, status = $2 WHERE id = $3`, active, status, domainID)
	return err
}

func (s *Store) BatchToggleDomains(ctx context.Context, ids []int, active bool) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	status := "disabled"
	if active {
		status = "active"
	}
	args := []any{active, status}
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	query := fmt.Sprintf(
		`UPDATE domains SET is_active = $1, status = $2 WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)
	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// UpdateDomainSubdomain 更新域名的多级子域配置（开关 + 随机长度）
func (s *Store) UpdateDomainSubdomain(ctx context.Context, domainID int, enabled bool, randomLength int) error {
	if randomLength < 2 || randomLength > 8 {
		randomLength = 5
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE domains SET subdomain_enabled = $1, subdomain_random_length = $2 WHERE id = $3`,
		enabled, randomLength, domainID,
	)
	return err
}

// BatchToggleDomainsSubdomain 批量启用/停用多级子域
func (s *Store) BatchToggleDomainsSubdomain(ctx context.Context, ids []int, enabled bool) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	args := []any{enabled}
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	query := fmt.Sprintf(
		`UPDATE domains SET subdomain_enabled = $1 WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)
	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// GetStats 返回全局统计数据
func (s *Store) GetStats(ctx context.Context) (*model.Stats, error) {
	var st model.Stats
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM mailboxes)                         AS total_mailboxes,
		  (SELECT COUNT(*) FROM mailboxes WHERE expires_at > NOW()) AS active_mailboxes,
		  (SELECT COUNT(*) FROM emails)                            AS total_emails,
		  (SELECT COUNT(*) FROM domains WHERE is_active = TRUE)    AS active_domains,
		  (SELECT COUNT(*) FROM domains WHERE status = 'pending')  AS pending_domains,
		  (SELECT COUNT(*) FROM accounts WHERE is_active = TRUE)   AS total_accounts
	`).Scan(
		&st.TotalMailboxes, &st.ActiveMailboxes,
		&st.TotalEmails, &st.ActiveDomains,
		&st.PendingDomains, &st.TotalAccounts,
	)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// ==================== Mailbox ====================

func (s *Store) CreateMailbox(ctx context.Context, accountID uuid.UUID, address string, domainID int, fullAddress string, ttlMinutes int) (*model.Mailbox, error) {
	if ttlMinutes <= 0 {
		ttlMinutes = 30
	}
	expiresAt := time.Now().Add(time.Duration(ttlMinutes) * time.Minute)
	var m model.Mailbox
	err := s.pool.QueryRow(ctx,
		`INSERT INTO mailboxes (account_id, address, domain_id, full_address, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, account_id, address, domain_id, full_address, is_favorite, tg_forward_enabled, created_at, expires_at, favorite_group_id`,
		accountID, address, domainID, fullAddress, expiresAt,
	).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.IsFavorite, &m.TGForwardEnabled, &m.CreatedAt, &m.ExpiresAt, &m.FavoriteGroupID)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// EnsureCatchAllMailbox 用于 catch-all 投递：地址不存在则自动创建并返回；
// 已存在（包括并发竞争）则返回既有行。注意：is_favorite/expires_at 不会被覆盖。
func (s *Store) EnsureCatchAllMailbox(ctx context.Context, accountID uuid.UUID, address string, domainID int, fullAddress string, ttlMinutes int) (*model.Mailbox, error) {
	if ttlMinutes <= 0 {
		ttlMinutes = 30
	}
	expiresAt := time.Now().Add(time.Duration(ttlMinutes) * time.Minute)
	var m model.Mailbox
	// ON CONFLICT 上 DO UPDATE 一个无副作用字段，保证 RETURNING 永远拿到行
	err := s.pool.QueryRow(ctx,
		`INSERT INTO mailboxes (account_id, address, domain_id, full_address, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (full_address) DO UPDATE SET full_address = EXCLUDED.full_address
		 RETURNING id, account_id, address, domain_id, full_address, is_favorite, tg_forward_enabled, created_at, expires_at, favorite_group_id`,
		accountID, address, domainID, fullAddress, expiresAt,
	).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.IsFavorite, &m.TGForwardEnabled, &m.CreatedAt, &m.ExpiresAt, &m.FavoriteGroupID)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// SetMailboxFavorite 设置/取消收藏。取消收藏时把 expires_at 重置为 now + ttl，避免立即被清理。
func (s *Store) SetMailboxFavorite(ctx context.Context, mailboxID uuid.UUID, accountID uuid.UUID, fav bool, ttlMinutes int) (*model.Mailbox, error) {
	if ttlMinutes <= 0 {
		ttlMinutes = 30
	}
	var m model.Mailbox
	var err error
	if fav {
		if err := s.ensureFavoriteGroup(ctx, accountID); err != nil {
			return nil, err
		}
		err = s.pool.QueryRow(ctx,
			`WITH selected_group AS (
				SELECT g.id
				FROM favorite_groups g
				LEFT JOIN favorite_group_preferences p
				  ON p.account_id = g.account_id AND p.group_id = g.id
				WHERE g.account_id = $2
				ORDER BY (p.group_id IS NOT NULL) DESC, g.sort_order, g.created_at, g.id
				LIMIT 1
			)
			UPDATE mailboxes
			 SET is_favorite = TRUE,
			     favorite_group_id = COALESCE(mailboxes.favorite_group_id, (SELECT id FROM selected_group))
			 WHERE id = $1 AND account_id = $2
			 RETURNING id, account_id, address, domain_id, full_address, is_favorite, tg_forward_enabled, created_at, expires_at, favorite_group_id`,
			mailboxID, accountID,
		).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.IsFavorite, &m.TGForwardEnabled, &m.CreatedAt, &m.ExpiresAt, &m.FavoriteGroupID)
	} else {
		newExpire := time.Now().Add(time.Duration(ttlMinutes) * time.Minute)
		err = s.pool.QueryRow(ctx,
			`UPDATE mailboxes SET is_favorite = FALSE, favorite_group_id = NULL, expires_at = $3
			 WHERE id = $1 AND account_id = $2
			 RETURNING id, account_id, address, domain_id, full_address, is_favorite, tg_forward_enabled, created_at, expires_at, favorite_group_id`,
			mailboxID, accountID, newExpire,
		).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.IsFavorite, &m.TGForwardEnabled, &m.CreatedAt, &m.ExpiresAt, &m.FavoriteGroupID)
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) SetMailboxTGForward(ctx context.Context, mailboxID uuid.UUID, accountID uuid.UUID, enabled bool) (*model.Mailbox, error) {
	var m model.Mailbox
	err := s.pool.QueryRow(ctx,
		`UPDATE mailboxes SET tg_forward_enabled = $3
		 WHERE id = $1 AND account_id = $2
		 RETURNING id, account_id, address, domain_id, full_address, is_favorite, tg_forward_enabled, created_at, expires_at, favorite_group_id`,
		mailboxID, accountID, enabled,
	).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.IsFavorite, &m.TGForwardEnabled, &m.CreatedAt, &m.ExpiresAt, &m.FavoriteGroupID)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetCatchAllAccountID 获取 catch-all 邮箱的归属账号：
// 优先读 catchall_account_id 设置；为空时回退首个 admin。
func (s *Store) GetCatchAllAccountID(ctx context.Context) (uuid.UUID, error) {
	configured, _ := s.GetSetting(ctx, "catchall_account_id")
	if configured != "" {
		if id, err := uuid.Parse(configured); err == nil {
			// 验证账号确实存在且活跃
			var ok bool
			if err := s.pool.QueryRow(ctx,
				`SELECT TRUE FROM accounts WHERE id = $1 AND is_active = TRUE`, id,
			).Scan(&ok); err == nil {
				return id, nil
			}
		}
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE is_admin = TRUE AND is_active = TRUE
		 ORDER BY created_at LIMIT 1`,
	).Scan(&id)
	return id, err
}

func (s *Store) ListMailboxes(ctx context.Context, accountID uuid.UUID, page, size int, folder string, favoriteGroupID *uuid.UUID) ([]model.Mailbox, int, error) {
	folder = strings.ToLower(strings.TrimSpace(folder))
	where := `account_id = $1`
	orderBy := `is_favorite DESC, created_at DESC`
	countArgs := []any{accountID}
	switch folder {
	case "temp":
		where += ` AND is_favorite = FALSE`
		orderBy = `created_at DESC`
	case "favorites":
		where += ` AND is_favorite = TRUE`
		orderBy = `created_at DESC`
		if favoriteGroupID != nil {
			where += ` AND favorite_group_id = $2`
			countArgs = append(countArgs, *favoriteGroupID)
		}
	default:
		folder = "all"
	}

	var total int
	err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM mailboxes WHERE %s`, where),
		countArgs...,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	limitPos := len(countArgs) + 1
	offsetPos := limitPos + 1
	queryArgs := append(append([]any{}, countArgs...), size, (page-1)*size)
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(
			`SELECT id, account_id, address, domain_id, full_address, is_favorite, tg_forward_enabled, created_at, expires_at, favorite_group_id
			 FROM mailboxes
			 WHERE %s
			 ORDER BY %s
			 LIMIT $%d OFFSET $%d`,
			where,
			orderBy,
			limitPos,
			offsetPos,
		),
		queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	mailboxes, err := pgx.CollectRows(rows, pgx.RowToStructByPos[model.Mailbox])
	if err != nil {
		return nil, 0, err
	}
	return mailboxes, total, nil
}

func (s *Store) GetMailbox(ctx context.Context, mailboxID uuid.UUID, accountID uuid.UUID) (*model.Mailbox, error) {
	var m model.Mailbox
	err := s.pool.QueryRow(ctx,
		`SELECT id, account_id, address, domain_id, full_address, is_favorite, tg_forward_enabled, created_at, expires_at, favorite_group_id
		 FROM mailboxes WHERE id = $1 AND account_id = $2`,
		mailboxID, accountID,
	).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.IsFavorite, &m.TGForwardEnabled, &m.CreatedAt, &m.ExpiresAt, &m.FavoriteGroupID)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) DeleteMailbox(ctx context.Context, mailboxID uuid.UUID, accountID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM mailboxes WHERE id = $1 AND account_id = $2`, mailboxID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Store) GetMailboxByFullAddress(ctx context.Context, fullAddress string) (*model.Mailbox, error) {
	var m model.Mailbox
	err := s.pool.QueryRow(ctx,
		`SELECT id, account_id, address, domain_id, full_address, is_favorite, tg_forward_enabled, created_at, expires_at, favorite_group_id
		 FROM mailboxes WHERE full_address = $1`,
		strings.ToLower(fullAddress),
	).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.IsFavorite, &m.TGForwardEnabled, &m.CreatedAt, &m.ExpiresAt, &m.FavoriteGroupID)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) GetMailboxByFullAddressForAccount(ctx context.Context, fullAddress string, accountID uuid.UUID) (*model.Mailbox, error) {
	var m model.Mailbox
	err := s.pool.QueryRow(ctx,
		`SELECT id, account_id, address, domain_id, full_address, is_favorite, tg_forward_enabled, created_at, expires_at, favorite_group_id
		 FROM mailboxes WHERE full_address = $1 AND account_id = $2`,
		strings.ToLower(strings.TrimSpace(fullAddress)), accountID,
	).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.IsFavorite, &m.TGForwardEnabled, &m.CreatedAt, &m.ExpiresAt, &m.FavoriteGroupID)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) GetMailboxOTPShare(ctx context.Context, mailboxID, accountID uuid.UUID) (*model.MailboxOTPShare, error) {
	var share model.MailboxOTPShare
	err := s.pool.QueryRow(ctx,
		`SELECT s.mailbox_id, m.full_address, s.token, s.created_at, s.updated_at
		 FROM mailbox_otp_shares s
		 JOIN mailboxes m ON m.id = s.mailbox_id
		 WHERE s.mailbox_id = $1 AND m.account_id = $2`,
		mailboxID, accountID,
	).Scan(&share.MailboxID, &share.FullAddress, &share.Token, &share.CreatedAt, &share.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &share, nil
}

func (s *Store) ListMailboxOTPShares(ctx context.Context, accountID uuid.UUID) ([]model.MailboxOTPShare, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT s.mailbox_id, m.full_address, s.token, s.created_at, s.updated_at
		 FROM mailbox_otp_shares s
		 JOIN mailboxes m ON m.id = s.mailbox_id
		 WHERE m.account_id = $1
		 ORDER BY s.updated_at DESC, m.full_address`,
		accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shares, err := pgx.CollectRows(rows, pgx.RowToStructByPos[model.MailboxOTPShare])
	if err != nil {
		return nil, err
	}
	return shares, nil
}

func (s *Store) UpsertMailboxOTPShare(ctx context.Context, mailboxID, accountID uuid.UUID, token string) (*model.MailboxOTPShare, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		token = generateMailboxOTPShareToken()
	}

	var existingMailboxID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT mailbox_id FROM mailbox_otp_shares WHERE token = $1`,
		token,
	).Scan(&existingMailboxID)
	if err == nil && existingMailboxID != mailboxID {
		return nil, ErrMailboxOTPShareTokenConflict
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	var share model.MailboxOTPShare
	err = s.pool.QueryRow(ctx,
		`WITH owned AS (
			SELECT id, full_address
			FROM mailboxes
			WHERE id = $1 AND account_id = $2
		),
		upserted AS (
			INSERT INTO mailbox_otp_shares (mailbox_id, token)
			SELECT owned.id, $3
			FROM owned
			ON CONFLICT (mailbox_id) DO UPDATE
				SET token = EXCLUDED.token,
				    updated_at = NOW()
			RETURNING mailbox_id, token, created_at, updated_at
		)
		SELECT upserted.mailbox_id, owned.full_address, upserted.token, upserted.created_at, upserted.updated_at
		FROM upserted
		JOIN owned ON owned.id = upserted.mailbox_id`,
		mailboxID, accountID, token,
	).Scan(&share.MailboxID, &share.FullAddress, &share.Token, &share.CreatedAt, &share.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "mailbox_otp_shares_token_key") {
			return nil, ErrMailboxOTPShareTokenConflict
		}
		return nil, err
	}
	return &share, nil
}

func (s *Store) DeleteMailboxOTPShare(ctx context.Context, mailboxID, accountID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM mailbox_otp_shares s
		 USING mailboxes m
		 WHERE s.mailbox_id = m.id
		   AND s.mailbox_id = $1
		   AND m.account_id = $2`,
		mailboxID, accountID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Store) GetMailboxOTPShareByToken(ctx context.Context, token string) (*model.MailboxOTPShare, error) {
	var share model.MailboxOTPShare
	err := s.pool.QueryRow(ctx,
		`SELECT s.mailbox_id, m.full_address, s.token, s.created_at, s.updated_at
		 FROM mailbox_otp_shares s
		 JOIN mailboxes m ON m.id = s.mailbox_id
		 WHERE s.token = $1`,
		strings.TrimSpace(token),
	).Scan(&share.MailboxID, &share.FullAddress, &share.Token, &share.CreatedAt, &share.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &share, nil
}

// DeleteExpiredMailboxes 刪除已过期的邮箱（及其所有邮件），收藏的邮箱永不删除。
func (s *Store) DeleteExpiredMailboxes(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM mailboxes WHERE expires_at < NOW() AND is_favorite = FALSE`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CheckDomainMX 检测域名MX记录是否指向指定服务器IP
func CheckDomainMX(domain, serverIP string) (matched bool, mxHosts []string, status string) {
	mxRecords, err := net.LookupMX(domain)
	if err != nil {
		return false, nil, fmt.Sprintf("DNS查询失败: %v", err)
	}
	if len(mxRecords) == 0 {
		return false, nil, "未找到MX记录，请先配置MX记录"
	}
	for _, mx := range mxRecords {
		host := strings.TrimSuffix(mx.Host, ".")
		mxHosts = append(mxHosts, host)
		addrs, err := net.LookupHost(host)
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if addr == serverIP {
				return true, mxHosts, fmt.Sprintf("✓ MX记录匹配：%s → %s", host, addr)
			}
		}
	}
	return false, mxHosts, fmt.Sprintf("MX记录(%s)未指向本服务器(%s)", strings.Join(mxHosts, ","), serverIP)
}

// CheckWildcardMX 通过对一个随机探测子域 probeXXXX.<domain> 做 MX 查询，
// 验证 *.<domain> 通配 MX 是否生效。返回探测使用的 FQDN 与状态串。
func CheckWildcardMX(domain, serverIP string) (matched bool, probe string, status string) {
	probe = "probe" + GenerateRandomSubdomain(6) + "." + strings.TrimSpace(strings.ToLower(domain))
	matched, _, status = CheckDomainMX(probe, serverIP)
	return matched, probe, status
}

// ==================== Email ====================

func (s *Store) InsertEmail(ctx context.Context, mailboxID uuid.UUID, sender, subject, bodyText, bodyHTML, raw string) (*model.Email, error) {
	var e model.Email
	err := s.pool.QueryRow(ctx,
		`INSERT INTO emails (mailbox_id, sender, subject, body_text, body_html, raw_message, size_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, mailbox_id, sender, subject, body_text, body_html, raw_message, size_bytes, received_at`,
		mailboxID, sender, subject, bodyText, bodyHTML, raw, len(raw),
	).Scan(&e.ID, &e.MailboxID, &e.Sender, &e.Subject, &e.BodyText, &e.BodyHTML, &e.RawMessage, &e.SizeBytes, &e.ReceivedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) ListEmails(ctx context.Context, mailboxID uuid.UUID, page, size int) ([]model.EmailSummary, int, error) {
	var total int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM emails WHERE mailbox_id = $1`, mailboxID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, sender, subject, size_bytes, received_at
		 FROM emails WHERE mailbox_id = $1
		 ORDER BY received_at DESC LIMIT $2 OFFSET $3`,
		mailboxID, size, (page-1)*size,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	emails, err := pgx.CollectRows(rows, pgx.RowToStructByPos[model.EmailSummary])
	if err != nil {
		return nil, 0, err
	}
	return emails, total, nil
}

func (s *Store) GetLatestEmail(ctx context.Context, mailboxID uuid.UUID) (*model.Email, error) {
	var e model.Email
	err := s.pool.QueryRow(ctx,
		`SELECT id, mailbox_id, sender, subject, body_text, body_html, raw_message, size_bytes, received_at
		 FROM emails
		 WHERE mailbox_id = $1
		 ORDER BY received_at DESC
		 LIMIT 1`,
		mailboxID,
	).Scan(&e.ID, &e.MailboxID, &e.Sender, &e.Subject, &e.BodyText, &e.BodyHTML, &e.RawMessage, &e.SizeBytes, &e.ReceivedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) GetEmail(ctx context.Context, emailID uuid.UUID, mailboxID uuid.UUID) (*model.Email, error) {
	var e model.Email
	err := s.pool.QueryRow(ctx,
		`SELECT id, mailbox_id, sender, subject, body_text, body_html, raw_message, size_bytes, received_at
		 FROM emails WHERE id = $1 AND mailbox_id = $2`,
		emailID, mailboxID,
	).Scan(&e.ID, &e.MailboxID, &e.Sender, &e.Subject, &e.BodyText, &e.BodyHTML, &e.RawMessage, &e.SizeBytes, &e.ReceivedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) DeleteEmail(ctx context.Context, emailID uuid.UUID, mailboxID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM emails WHERE id = $1 AND mailbox_id = $2`, emailID, mailboxID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ==================== Helpers ====================

func generateAPIKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "tm_" + hex.EncodeToString(b)
}

func generateMailboxOTPShareToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "tms_" + hex.EncodeToString(b)
}

func GenerateRandomAddress() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	length := 10
	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[n.Int64()]
	}
	return string(result)
}
