-- ============================================================
-- TempMail v7 迁移：API 创建邮箱独立 TTL
-- 在已部署的库上执行：psql ... -f sql/migrate_v7.sql
-- 幂等，可重复执行；老库无需停服
-- ============================================================

BEGIN;

-- 通过 API 创建邮箱时使用的有效期（分钟）。留空 = 复用 mailbox_ttl_minutes。
INSERT INTO app_settings (key, value) VALUES ('api_mailbox_ttl_minutes', '')
    ON CONFLICT (key) DO NOTHING;

COMMIT;
