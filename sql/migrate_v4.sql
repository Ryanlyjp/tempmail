-- ============================================================
-- TempMail v4 迁移：catch-all 收件 + 收藏邮箱
-- 在已部署的库上执行：psql ... -f sql/migrate_v4.sql
-- 幂等，可重复执行
-- ============================================================

-- 收藏邮箱：被收藏的邮箱不会被定时清理任务删除
ALTER TABLE mailboxes
    ADD COLUMN IF NOT EXISTS is_favorite BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_mailboxes_favorite
    ON mailboxes (is_favorite) WHERE is_favorite = TRUE;

-- catch-all 设置项
INSERT INTO app_settings (key, value) VALUES ('catchall_enabled', 'false')
    ON CONFLICT (key) DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('catchall_account_id', '')
    ON CONFLICT (key) DO NOTHING;
