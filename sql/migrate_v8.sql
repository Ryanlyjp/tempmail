-- ============================================================
-- TempMail v8 迁移：Hostname 资源化管理
-- 在已部署的库上执行：psql ... -f sql/migrate_v8.sql
-- 幂等，可重复执行；老库无需停服
-- ============================================================

BEGIN;

CREATE TABLE IF NOT EXISTS hostnames (
    id          SERIAL PRIMARY KEY,
    hostname    VARCHAR(255) NOT NULL UNIQUE,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hostnames_active
    ON hostnames (is_active) WHERE is_active = TRUE;

ALTER TABLE domains
    ADD COLUMN IF NOT EXISTS hostname_id INT;

ALTER TABLE domains
    DROP CONSTRAINT IF EXISTS domains_hostname_id_fkey;

ALTER TABLE domains
    ADD CONSTRAINT domains_hostname_id_fkey
    FOREIGN KEY (hostname_id) REFERENCES hostnames(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_domains_hostname_id
    ON domains (hostname_id);

INSERT INTO app_settings (key, value) VALUES ('smtp_hostname', '')
    ON CONFLICT (key) DO NOTHING;

INSERT INTO hostnames (hostname)
SELECT LOWER(TRIM(value))
FROM app_settings
WHERE key = 'smtp_hostname' AND TRIM(value) <> ''
ON CONFLICT (hostname) DO NOTHING;

INSERT INTO hostnames (hostname)
SELECT DISTINCT LOWER(TRIM(hostname))
FROM domains
WHERE TRIM(hostname) <> ''
ON CONFLICT (hostname) DO NOTHING;

UPDATE domains d
SET hostname_id = h.id,
    hostname = h.hostname
FROM hostnames h
WHERE TRIM(d.hostname) <> ''
  AND LOWER(TRIM(d.hostname)) = h.hostname
  AND (d.hostname_id IS NULL OR d.hostname <> h.hostname);

COMMIT;
