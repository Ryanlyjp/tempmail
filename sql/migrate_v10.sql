-- Optional segmented OTP recognition (ABC-DEF / ABCD-12EF).
-- Disabled by default so existing extraction behavior remains unchanged.

BEGIN;

INSERT INTO app_settings (key, value) VALUES ('otp_segmented_enabled', 'false')
ON CONFLICT (key) DO NOTHING;

INSERT INTO app_settings (key, value) VALUES ('otp_segmented_lengths', '3')
ON CONFLICT (key) DO NOTHING;

INSERT INTO app_settings (key, value) VALUES ('otp_segmented_senders', '')
ON CONFLICT (key) DO NOTHING;

COMMIT;
