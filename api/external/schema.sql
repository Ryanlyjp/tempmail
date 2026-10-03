CREATE TABLE IF NOT EXISTS external_accounts (
 id UUID PRIMARY KEY,
 config JSONB NOT NULL,
 credentials BYTEA NOT NULL,
 enabled BOOLEAN NOT NULL DEFAULT TRUE,
 tg_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 initial_since TIMESTAMPTZ NOT NULL DEFAULT NOW() - INTERVAL '3 days',
 last_sync TIMESTAMPTZ,
 imported_at TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT '',
 requested BOOLEAN NOT NULL DEFAULT TRUE,
 lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE external_accounts ADD COLUMN IF NOT EXISTS tg_enabled BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE IF NOT EXISTS external_mailboxes (
 id UUID PRIMARY KEY,
 account_id UUID NOT NULL REFERENCES external_accounts(id) ON DELETE CASCADE,
 address TEXT NOT NULL,
 tg_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 UNIQUE(account_id,address)
);
CREATE TABLE IF NOT EXISTS external_messages (
 id UUID PRIMARY KEY,
 account_id UUID NOT NULL REFERENCES external_accounts(id) ON DELETE CASCADE,
 source_key TEXT NOT NULL,
 recipient TEXT NOT NULL DEFAULT '',
 message JSONB NOT NULL,
 raw BYTEA NOT NULL,
 received_at TIMESTAMPTZ NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 promotion BOOLEAN NOT NULL DEFAULT FALSE,
 notified BOOLEAN NOT NULL DEFAULT FALSE,
 seen_run UUID NOT NULL,
 UNIQUE(account_id,source_key)
);
CREATE INDEX IF NOT EXISTS external_messages_recipient ON external_messages(account_id,recipient,received_at DESC) WHERE active;
CREATE TABLE IF NOT EXISTS external_sources (
 account_id UUID NOT NULL REFERENCES external_accounts(id) ON DELETE CASCADE,
 source_ref TEXT NOT NULL,
 message_id UUID NOT NULL REFERENCES external_messages(id) ON DELETE CASCADE,
 PRIMARY KEY(account_id,source_ref)
);
CREATE TABLE IF NOT EXISTS external_shares (
 mailbox_id UUID PRIMARY KEY REFERENCES external_mailboxes(id) ON DELETE CASCADE,
 api_key TEXT NOT NULL UNIQUE,
 enabled BOOLEAN NOT NULL DEFAULT TRUE,
 expires_at TIMESTAMPTZ,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
