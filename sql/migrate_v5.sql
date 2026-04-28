-- ============================================================
-- TempMail v5 迁移：每域名 hostname + Cloudflare 设置
-- 在已部署的库上执行：psql ... -f sql/migrate_v5.sql
-- 幂等，可重复执行
-- ============================================================

ALTER TABLE domains
    ADD COLUMN IF NOT EXISTS hostname VARCHAR(255) NOT NULL DEFAULT '';

INSERT INTO app_settings (key, value) VALUES ('cf_api_token', '')
    ON CONFLICT (key) DO NOTHING;
