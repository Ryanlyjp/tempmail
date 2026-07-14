-- ============================================================
-- TempMail 临时邮箱平台 - 数据库初始化
-- 针对高并发优化：索引、分区就绪、UUID主键
-- ============================================================

-- 启用扩展
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================
-- 1. 账号表 (accounts)
-- ============================================================
CREATE TABLE accounts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username    VARCHAR(64)  NOT NULL UNIQUE,
    api_key     VARCHAR(64)  NOT NULL UNIQUE,
    is_admin    BOOLEAN      NOT NULL DEFAULT FALSE,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- API Key 查询走 B-tree 索引（认证热路径）
CREATE INDEX idx_accounts_api_key ON accounts (api_key);

-- ============================================================
-- 2. Hostname 池表 (hostnames)
-- ============================================================
CREATE TABLE hostnames (
    id          SERIAL PRIMARY KEY,
    hostname    VARCHAR(255) NOT NULL UNIQUE,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_hostnames_active ON hostnames (is_active) WHERE is_active = TRUE;

-- ============================================================
-- 3. 域名池表 (domains)
-- ============================================================
CREATE TABLE domains (
    id                       SERIAL PRIMARY KEY,
    domain                   VARCHAR(255) NOT NULL UNIQUE,
    hostname                 VARCHAR(255) NOT NULL DEFAULT '',
    hostname_id              INT REFERENCES hostnames(id) ON DELETE SET NULL,
    is_active                BOOLEAN      NOT NULL DEFAULT TRUE,
    status                   VARCHAR(16)  NOT NULL DEFAULT 'active',  -- active / pending / disabled
    subdomain_enabled        BOOLEAN      NOT NULL DEFAULT FALSE,
    subdomain_random_length  INT          NOT NULL DEFAULT 5
        CHECK (subdomain_random_length BETWEEN 2 AND 8),
    mx_checked_at TIMESTAMPTZ,                             -- 最近一次 MX 检测时间
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_domains_active ON domains (is_active) WHERE is_active = TRUE;
CREATE INDEX idx_domains_status ON domains (status) WHERE status = 'pending';
CREATE INDEX idx_domains_subdomain_enabled ON domains (subdomain_enabled) WHERE subdomain_enabled = TRUE;
CREATE INDEX idx_domains_hostname_id ON domains (hostname_id);

-- ============================================================
-- 4. 邮箱表 (mailboxes)
-- ============================================================
CREATE TABLE mailboxes (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   UUID         NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    address      VARCHAR(128) NOT NULL,  -- 本地部分，如 "abc123"
    domain_id    INT          NOT NULL REFERENCES domains(id),
    full_address VARCHAR(320) NOT NULL,  -- 完整地址 "abc123@mail.xxx.xyz"
    is_favorite  BOOLEAN      NOT NULL DEFAULT FALSE,
    tg_forward_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW() + INTERVAL '30 minutes'
);

-- 完整地址唯一索引（收件匹配热路径）
CREATE UNIQUE INDEX idx_mailboxes_full_address ON mailboxes (full_address);

-- 按账号查邮箱列表
CREATE INDEX idx_mailboxes_account_id ON mailboxes (account_id);

-- 过期自动清理索引
CREATE INDEX idx_mailboxes_expires_at ON mailboxes (expires_at);

-- 收藏邮箱索引（清理任务跳过收藏邮箱时用）
CREATE INDEX idx_mailboxes_favorite ON mailboxes (is_favorite) WHERE is_favorite = TRUE;

-- 收藏夹分组
CREATE TABLE favorite_groups (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name         VARCHAR(64) NOT NULL,
    sort_order   INT         NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_favorite_groups_account_name
    ON favorite_groups (account_id, LOWER(name));
CREATE INDEX idx_favorite_groups_account_sort
    ON favorite_groups (account_id, sort_order, created_at);

ALTER TABLE mailboxes
    ADD COLUMN favorite_group_id UUID REFERENCES favorite_groups(id) ON DELETE SET NULL;
CREATE INDEX idx_mailboxes_favorite_group
    ON mailboxes (account_id, favorite_group_id, created_at DESC)
    WHERE is_favorite = TRUE;

CREATE TABLE favorite_group_preferences (
    account_id UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    group_id   UUID REFERENCES favorite_groups(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- 5. 邮件表 (emails)
-- ============================================================
CREATE TABLE emails (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mailbox_id   UUID         NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    sender       VARCHAR(320) NOT NULL DEFAULT '',
    subject      VARCHAR(998) NOT NULL DEFAULT '',
    body_text    TEXT         NOT NULL DEFAULT '',
    body_html    TEXT         NOT NULL DEFAULT '',
    raw_message  TEXT         NOT NULL DEFAULT '',
    size_bytes   INT          NOT NULL DEFAULT 0,
    received_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- 按邮箱查邮件（分页查询热路径）
CREATE INDEX idx_emails_mailbox_received ON emails (mailbox_id, received_at DESC);

-- ============================================================
-- 6. 初始管理员账号
-- ============================================================
INSERT INTO accounts (username, api_key, is_admin)
VALUES ('admin', 'tm_admin_' || encode(gen_random_bytes(24), 'hex'), TRUE);

-- ============================================================
-- 7. 初始域名（请在启动后通过管理后台或 API 添加实际域名）
-- ============================================================
-- INSERT INTO domains (domain) VALUES ('mail.yourdomain.com');

-- ============================================================
-- 8. 应用设置表 (app_settings)
-- ============================================================
CREATE TABLE IF NOT EXISTS app_settings (
    key        VARCHAR(64) PRIMARY KEY,
    value      TEXT        NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO app_settings (key, value) VALUES ('registration_open', 'true') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('smtp_server_ip', '') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('smtp_hostname', '') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('mailbox_ttl_minutes', '30') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('otp_segmented_enabled', 'false') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('otp_segmented_lengths', '3') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('otp_segmented_senders', '') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('catchall_enabled', 'false') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('catchall_account_id', '') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('cf_api_token', '') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('api_subdomain_enabled', 'false') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('api_subdomain_length', '5') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('api_domain_strategy', 'random') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('api_domain_fixed', '') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('api_mailbox_ttl_minutes', '') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('tg_bot_token', '') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('tg_chat_id', '') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('tg_message_thread_id', '') ON CONFLICT DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('tg_forward_mode', 'all_with_attachments') ON CONFLICT DO NOTHING;

-- ============================================================
-- 9. 数据库性能参数（在 postgresql.conf 或 docker 环境变量中设置更佳）
-- ============================================================
-- 以下通过 ALTER SYSTEM 设置，重启后生效
-- ALTER SYSTEM SET shared_buffers = '256MB';
-- ALTER SYSTEM SET effective_cache_size = '512MB';
-- ALTER SYSTEM SET work_mem = '4MB';
-- ALTER SYSTEM SET maintenance_work_mem = '64MB';
-- ALTER SYSTEM SET max_connections = 200;
