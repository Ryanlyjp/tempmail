BEGIN;

UPDATE mailbox_otp_shares
SET api_key = token
WHERE api_key IS NULL OR BTRIM(api_key) = '';

UPDATE mailbox_otp_shares
SET token = api_key
WHERE token IS DISTINCT FROM api_key;

ALTER TABLE mailbox_otp_shares
    ALTER COLUMN api_key SET NOT NULL;

COMMIT;
