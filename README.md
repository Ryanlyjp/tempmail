# TempMail

一个自托管临时邮箱平台，当前版本基于 `PostgreSQL + PgBouncer + Redis + Go API + Nginx + Postfix`，并在原始项目基础上扩展了收藏夹、附件解析下载、Telegram 转发、域名批量管理、多级子域名建箱等能力。

---

## 当前能力

- 临时邮箱默认按 TTL 自动过期清理
- 收藏邮箱单独进入“收藏夹”，不会被自动清理
- 左栏邮箱分页显示，临时箱 / 收藏夹分开展示
- 三栏 Dashboard：邮箱列表 / 邮件列表 / 邮件正文
- 现有邮件支持补解析附件，不新增附件内容入库
- 网页端可直接下载附件
- 支持提取最新一封邮件 OTP
- 支持每个邮箱独立开启/关闭 Telegram 转发
- 支持 Telegram 全局转发策略、测试消息、手动转发已有邮件
- 支持仅转发带附件邮件、仅通知带附件邮件
- TG 文本会标明来自哪个邮箱
- 支持 catch-all 自动建箱
- 支持多个 hostname 统一管理、启停、编辑、删除
- 支持多级子域名邮箱创建
- 支持域名 MX 自动验证、健康巡检、批量启停/删除
- 支持 Cloudflare MX 自动创建 / 删除

---

## 架构

- `postgres`: 主数据库
- `pgbouncer`: PostgreSQL 连接池
- `redis`: 限流与缓存
- `api`: Go 后端
- `frontend`: Nginx 托管 SPA 并反代 API
- `postfix`: SMTP 收件

`docker-compose.yml` 当前使用本地源码构建：

- `api` -> `build: ./api`
- `postfix` -> `build: ./postfix`

---

## 快速启动

### 1. 准备环境

```bash
git clone <repo-url>
cd tempmail
cp .env.example .env
```

至少需要填写这些变量：

```dotenv
POSTGRES_DB=tempmail
POSTGRES_USER=tempmail
POSTGRES_PASSWORD=change_me

API_DB_DSN=postgres://tempmail:change_me@pgbouncer:6432/tempmail?sslmode=disable
API_REDIS_ADDR=redis:6379
API_REDIS_PASSWORD=change_me
API_RATE_LIMIT=500
API_RATE_WINDOW=60
API_PORT=8080

SMTP_SERVER_IP=1.2.3.4
SMTP_HOSTNAME=mail.yourdomain.com
REDIS_PASSWORD=change_me
```

### 2. 启动

```bash
docker compose up -d --build
```

### 3. 获取管理员 API Key

首次启动后会写入：

```bash
cat data/admin.key
```

### 4. 访问页面

```text
http://<服务器IP>
```

---

## 邮箱行为

### 临时邮箱

- 默认 TTL 由 `mailbox_ttl_minutes` 控制
- 过期后由后台清理器删除
- Web 创建的邮箱使用普通 TTL
- 非 Web / API 创建可单独使用 `api_mailbox_ttl_minutes`

### 收藏夹

- 收藏后的邮箱会从“临时邮箱”移动到“收藏夹”
- 收藏夹中的邮箱永久保留，不参与过期清理
- 取消收藏后会重新写回 `expires_at = now + mailbox_ttl_minutes`
- 左栏默认显示临时邮箱，点击 `★ 收藏夹` 可切换视图
- 为兼容旧调用，API 默认仍返回全部邮箱；前端通过 `folder` 参数按文件夹过滤

### Catch-all 自动建箱

当 `catchall_enabled=true` 时，发往已托管域名但未预创建地址的邮件会：

1. 校验域名已在系统中且为激活状态
2. 归属给 `catchall_account_id` 或第一个管理员账号
3. 自动创建邮箱
4. 按正常流程继续收信

相关设置：

- `catchall_enabled`
- `catchall_account_id`
- `mailbox_ttl_minutes`

---

## 附件行为

- 附件内容不单独存数据库
- 邮件原文仍保存在 `emails.raw_message`
- 读取邮件详情或下载附件时，后端会实时从原始 MIME 邮件里解析附件
- 这意味着已有老邮件也支持“补解析附件”，不需要额外迁移历史附件数据

当前能力：

- 邮件详情页显示附件数量与文件名
- 点击即可下载单个附件
- TG 手动转发已有邮件时也会重新从原始邮件解析附件

---

## Telegram 转发

### 配置项

管理员设置页支持：

- `tg_bot_token`
- `tg_chat_id`
- `tg_message_thread_id`
- `tg_forward_mode`

### 全局转发模式

- `subject_only`: 只转发邮箱 / 发件人 / 标题 / 时间
- `important_without_attachments`: 转发去噪后的重要正文，不上传附件
- `important_with_attachments`: 转发去噪后的重要正文，并上传附件
- `notify_all`: 所有邮件只通知，不带正文
- `all_with_attachments`: 完整正文 + 附件
- `all_without_attachments`: 完整正文，不带附件
- `attachments_only`: 仅带附件邮件才转发，且上传附件
- `notify_attachments`: 仅带附件邮件才通知，不上传附件

### 使用方式

- 每个邮箱可单独开启 / 关闭 TG 转发
- 收到新邮件时按“邮箱开关 + 全局模式”共同决定是否发送
- 邮件正文会带上邮箱地址、发件人、主题、时间、附件数
- 重点正文模式会优先提取 OTP、验证/登录类链接、较短的关键句，并默认关闭 Telegram 链接预览卡片
- 设置页可发送一条 TG 测试消息验证连通性
- 单封已有邮件可手动补转发到 TG

---

## 多级子域名邮箱

创建邮箱时支持：

- `subdomain_mode=off`
- `subdomain_mode=random`
- `subdomain_mode=custom`

规则：

- 自定义子域仅允许 `a-z0-9`
- 长度限制 `2-8`
- 若目标域名未开启多级子域，会自动回退到普通邮箱地址

相关配置：

- `api_subdomain_enabled`
- `api_subdomain_length`
- `api_domain_strategy`
- `api_domain_fixed`

---

## 域名管理

普通用户可以：

- 查看共享域名池
- 提交域名进入 MX 自动验证
- 查询域名验证状态

管理员还可以：

- 维护 hostname 池（新增 / 编辑 / 启用 / 停用 / 删除）
- 手动添加域名
- 强制导入域名
- 为每个域名下拉选择 `hostname`
- 批量启用 / 停用 / 删除域名
- 批量开关多级子域名
- 通过 Cloudflare 创建 / 删除 MX
- 按 `status` / `hostname` / 关键字筛选域名

域名 `hostname` 的优先级：

1. 域名自身 `hostname`
2. 已启用 hostname 列表中的默认 hostname（按创建时间最早的启用项）
3. 默认回退 `mail.<domain>`

---

## API 使用

### 认证

受保护接口使用：

```http
Authorization: Bearer tm_xxxxxxxxxxxx
```

也兼容：

```text
?api_key=tm_xxxxxxxxxxxx
```

示例：

```bash
BASE="http://<服务器IP>"
KEY="tm_xxxxxxxxxxxx"
```

### 公开接口

```bash
curl "$BASE/public/settings"
curl "$BASE/public/stats"
```

### 邮箱接口

```bash
# 创建邮箱
curl -s -X POST "$BASE/api/mailboxes" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"address":"demo","domain":"example.com"}'

# 列出全部邮箱（兼容旧行为）
curl -s "$BASE/api/mailboxes?page=1&size=20" \
  -H "Authorization: Bearer $KEY"

# 只看临时邮箱
curl -s "$BASE/api/mailboxes?page=1&size=7&folder=temp" \
  -H "Authorization: Bearer $KEY"

# 只看收藏夹
curl -s "$BASE/api/mailboxes?page=1&size=7&folder=favorites" \
  -H "Authorization: Bearer $KEY"

# 收藏 / 取消收藏
curl -s -X PUT "$BASE/api/mailboxes/<mailbox-id>/favorite" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"favorite":true}'

# 开启 / 关闭该邮箱的 TG 转发
curl -s -X PUT "$BASE/api/mailboxes/<mailbox-id>/forward" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"enabled":true}'
```

### 邮件接口

```bash
# 列出邮件
curl -s "$BASE/api/mailboxes/<mailbox-id>/emails?page=1&size=20" \
  -H "Authorization: Bearer $KEY"

# 获取单封邮件详情（返回附件元信息，如可解析）
curl -s "$BASE/api/mailboxes/<mailbox-id>/emails/<email-id>" \
  -H "Authorization: Bearer $KEY"

# 下载附件
curl -L "$BASE/api/mailboxes/<mailbox-id>/emails/<email-id>/attachments/1" \
  -H "Authorization: Bearer $KEY" \
  -o attachment.bin

# 手动转发已有邮件到 Telegram
curl -s -X POST "$BASE/api/mailboxes/<mailbox-id>/emails/<email-id>/forward/tg" \
  -H "Authorization: Bearer $KEY"

# 提取最新一封邮件 OTP
curl -s "$BASE/api/mailboxes/<mailbox-id>/otp/latest" \
  -H "Authorization: Bearer $KEY"
```

### 域名接口

```bash
# 查看共享域名池（支持过滤）
curl -s "$BASE/api/domains?status=active&hostname=mail.example.com&q=example" \
  -H "Authorization: Bearer $KEY"

# 查看当前可选的 hostname 下拉列表
curl -s "$BASE/api/hostnames" \
  -H "Authorization: Bearer $KEY"

# 普通用户提交域名验证
curl -s -X POST "$BASE/api/domains/submit" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com","hostname_id":1}'

# 查询域名状态
curl -s "$BASE/api/domains/<domain-id>/status" \
  -H "Authorization: Bearer $KEY"
```

### 管理接口

```bash
# 读取系统设置
curl -s "$BASE/api/admin/settings" \
  -H "Authorization: Bearer $KEY"

# 更新 TG 设置
curl -s -X PUT "$BASE/api/admin/settings" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "tg_bot_token":"123456:ABCDEF",
    "tg_chat_id":"-1001234567890",
    "tg_message_thread_id":"",
    "tg_forward_mode":"all_with_attachments"
  }'

# 发送 TG 测试消息
curl -s -X POST "$BASE/api/admin/settings/tg/test" \
  -H "Authorization: Bearer $KEY"

# 查看全部 hostname
curl -s "$BASE/api/admin/hostnames" \
  -H "Authorization: Bearer $KEY"

# 新增 hostname
curl -s -X POST "$BASE/api/admin/hostnames" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"hostname":"mail.example.com"}'

# 编辑 hostname
curl -s -X PUT "$BASE/api/admin/hostnames/1" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"hostname":"mx1.example.com"}'

# 启用 / 停用 hostname
curl -s -X PUT "$BASE/api/admin/hostnames/1/toggle" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"active":false}'

# 删除 hostname
curl -s -X DELETE "$BASE/api/admin/hostnames/1" \
  -H "Authorization: Bearer $KEY"

# 手动添加域名
curl -s -X POST "$BASE/api/admin/domains" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com","hostname_id":1}'

# 更新域名 hostname
curl -s -X PUT "$BASE/api/admin/domains/<domain-id>/hostname" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"hostname_id":1}'

# MX 导入
curl -s -X POST "$BASE/api/admin/domains/mx-import" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com","hostname_id":1,"force":false}'

# MX 自动注册
curl -s -X POST "$BASE/api/admin/domains/mx-register" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com","hostname_id":1}'
```

---

## 运行与迁移说明

- API 启动时会自动执行一组 schema compatibility 检查
- 常见新增字段（如 `expires_at`、`is_favorite`、`tg_forward_enabled`、`hostname_id`、TG 设置项）会自动补齐
- 一般不需要再手工跑旧版增量 SQL 才能启动
- 如果是老库手工迁移，可执行 `sql/migrate_v8.sql` 把旧的 `smtp_hostname` / 域名 `hostname` 导入到新的 hostname 池
- 附件解析基于历史 `raw_message`，不需要额外迁移已有邮件内容

---

## 端口说明

- `80` 对外提供 Web
- `25` 对外提供 SMTP 收信，生产环境通常必须开放
- `8080` 为 API 容器端口，可按需决定是否暴露到公网

如果要改端口，请同时检查：

- `.env`
- `docker-compose.yml`
- `nginx/default.conf`
- `postfix/entrypoint.sh`
- `postfix/mail-receiver.py`

可直接参考仓库里的 [.env.example](./.env.example) 注释。
