BEGIN;

ALTER TABLE mailbox_otp_shares
    ADD COLUMN IF NOT EXISTS api_key VARCHAR(96);
ALTER TABLE mailbox_otp_shares
    ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE mailbox_otp_shares
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS idx_mailbox_otp_shares_api_key
    ON mailbox_otp_shares (api_key) WHERE api_key IS NOT NULL;

COMMIT;
