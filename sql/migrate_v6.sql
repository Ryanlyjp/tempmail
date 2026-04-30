-- ============================================================
-- TempMail v6 迁移：多级子域名（V2 寻址）
-- 在已部署的库上执行：psql ... -f sql/migrate_v6.sql
-- 幂等，可重复执行；老库无需停服
-- ============================================================

BEGIN;

ALTER TABLE domains
    ADD COLUMN IF NOT EXISTS subdomain_enabled BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE domains
    ADD COLUMN IF NOT EXISTS subdomain_random_length INT NOT NULL DEFAULT 5;

-- 校验：长度只允许 2-8（前端/后端规则一致）
ALTER TABLE domains
    DROP CONSTRAINT IF EXISTS domains_subdomain_random_length_chk;
ALTER TABLE domains
    ADD CONSTRAINT domains_subdomain_random_length_chk
    CHECK (subdomain_random_length BETWEEN 2 AND 8);

-- 后缀匹配（catch-all 路径）会按此索引筛选候选 base 域名
CREATE INDEX IF NOT EXISTS idx_domains_subdomain_enabled
    ON domains (subdomain_enabled) WHERE subdomain_enabled = TRUE;

-- 全局 API 默认配置（缺省即注入；已有值不覆盖）
INSERT INTO app_settings (key, value) VALUES ('api_subdomain_enabled', 'false')
    ON CONFLICT (key) DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('api_subdomain_length', '5')
    ON CONFLICT (key) DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('api_domain_strategy', 'random')
    ON CONFLICT (key) DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('api_domain_fixed', '')
    ON CONFLICT (key) DO NOTHING;

COMMIT;
