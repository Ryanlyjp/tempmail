-- Preserve every existing key; allow full mailbox names in newly generated keys.
ALTER TABLE mailbox_otp_shares
    ALTER COLUMN token TYPE VARCHAR(256),
    ALTER COLUMN api_key TYPE VARCHAR(256);
-- Independent external_* tables are initialized by api/external/schema.sql.
