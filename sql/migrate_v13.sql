BEGIN;

CREATE TABLE IF NOT EXISTS webhook_outbox (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email_id        UUID        NOT NULL UNIQUE,
    payload         JSONB       NOT NULL,
    attempts        INT         NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_webhook_outbox_due
    ON webhook_outbox (next_attempt_at, created_at);

INSERT INTO app_settings (key, value) VALUES
    ('tgmag_webhook_enabled', 'false'),
    ('tgmag_webhook_url', ''),
    ('tgmag_webhook_secret', ''),
    ('tgmag_webhook_domains', '[]')
ON CONFLICT (key) DO NOTHING;

COMMIT;
