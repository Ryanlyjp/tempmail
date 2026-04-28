# TempMail

一个自托管临时邮件平台，基于 `PostgreSQL + PgBouncer + Redis + Go API + Nginx + Postfix`。当前版本已经明显偏离原始项目，重点能力包括：

- catch-all 自动建箱
- 收藏邮箱防过期
- 三栏式邮箱总览 UI
- 域名 MX 自动验证与健康巡检
- 每域名可选 `hostname`
- Cloudflare MX 自动创建 / 删除
- 域名批量管理、筛选与状态统计
- 最新一封邮件 OTP 提取 API

---

## 功能概览

| 功能 | 说明 |
|------|------|
| 临时邮箱 | 创建临时邮箱，默认 TTL 30 分钟，自动清理 |
| Catch-all | 已托管域名下的未知地址可自动建箱并落到指定账号 |
| 收藏邮箱 | 收藏后不参与过期清理，取消收藏后恢复 TTL |
| 三栏 Dashboard | 左栏邮箱、中栏邮件列表、右栏邮件正文，支持快速取码 |
| OTP 提取 | 前端一键取码，后端提供最新邮件 OTP API |
| 多域名池 | 可指定域名建箱，也可随机选取激活域名 |
| 域名验证 | 提交域名后后台每 30 秒轮询 MX，通过即自动激活 |
| 域名巡检 | 每 6 小时重检激活域名，MX 失效会自动停用 |
| 每域名 Hostname | 域名可单独指定 `hostname`，为空时回退全局 `smtp_hostname` |
| Cloudflare 集成 | 管理员可通过 API / 后台自动创建、删除 MX 记录 |
| 域名增强管理 | 支持筛选、状态统计、批量启用/停用/删除 |
| API Key 鉴权 | 使用 `Authorization: Bearer <API_KEY>`，也兼容 `?api_key=` |
| 管理后台 | 管理账户、域名、系统设置、公告、Cloudflare Token |
| 高并发架构 | Redis 限流 + PgBouncer 事务池 + Go API |

---

## 部署架构

服务组成：

- `postgres`: 主数据库
- `pgbouncer`: PostgreSQL 连接池
- `redis`: 速率限制与缓存
- `api`: Go 后端
- `frontend`: Nginx 托管静态 SPA 并反代 API
- `postfix`: SMTP 收件

当前 `docker-compose.yml` 已改为本地构建镜像，源码改动会直接参与构建：

- `api` → `build: ./api`
- `postfix` → `build: ./postfix`

---

## 快速启动

### 前置条件

- Docker 20.10+
- Docker Compose v2+
- 可接收邮件的公网 IP

### 1. 克隆并配置

```bash
git clone <repo-url>
cd tempmail
cp .env.example .env
```

至少填写：

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

### 2. 启动服务

```bash
docker compose up -d --build
```

### 3. 获取管理员 API Key

首次启动后，管理员 Key 会写入 `data/admin.key`：

```bash
cat data/admin.key
```

### 4. 访问 Web

浏览器打开：

```text
http://<服务器IP>
```

---

## 核心行为

### 1. Catch-all 自动建箱

当 `catchall_enabled=true` 时，发往“已托管域名但未事先创建”的地址的邮件不会丢弃，而是：

1. 检查收件域名是否在本系统中且处于激活状态
2. 按 `catchall_account_id` 或首个管理员账号归属
3. 自动创建该邮箱
4. 继续按正常流程落邮件

相关系统设置：

- `catchall_enabled`
- `catchall_account_id`
- `mailbox_ttl_minutes`

### 2. 收藏邮箱

- 收藏邮箱后不会被后台过期清理器删除
- 取消收藏后，`expires_at` 会重置为 `now + mailbox_ttl_minutes`

### 3. 每域名 Hostname

域名可单独设置 `hostname`：

- 若域名自身 `hostname` 非空，优先使用
- 否则回退系统设置 `smtp_hostname`
- 若仍为空，则回退 `mail.<domain>`

这会影响：

- DNS 提示
- MX 自动注册提示
- Cloudflare MX 创建 / 删除目标

---

## 域名管理

### 用户侧

普通登录用户可以：

- 查看共享域名池
- 提交域名进入 MX 自动验证流程
- 轮询域名状态

### 管理员侧

管理员还可以：

- 手动添加域名
- 强制导入域名
- 更新单个域名 `hostname`
- 通过 Cloudflare 自动创建 MX
- 删除 Cloudflare MX 并删除本地域名
- 批量启用 / 停用 / 删除域名
- 按 `status` / `hostname` / 关键字筛选域名

---

## API 使用

### 认证方式

所有受保护 API 使用：

```http
Authorization: Bearer tm_xxxxxxxxxxxx
```

也兼容：

```text
?api_key=tm_xxxxxxxxxxxx
```

### 常用接口

```bash
BASE="http://<服务器IP>"
KEY="tm_xxxxxxxxxxxx"
```

公开接口：

```bash
curl "$BASE/public/settings"
curl "$BASE/public/stats"
```

基础邮箱接口：

```bash
# 创建邮箱
curl -s -X POST "$BASE/api/mailboxes" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"address":"test","domain":"example.com"}'

# 列出邮箱
curl -s "$BASE/api/mailboxes?page=1&size=20" \
  -H "Authorization: Bearer $KEY"

# 列出某邮箱邮件
curl -s "$BASE/api/mailboxes/<mailbox-id>/emails?page=1&size=20" \
  -H "Authorization: Bearer $KEY"

# 读取单封邮件
curl -s "$BASE/api/mailboxes/<mailbox-id>/emails/<email-id>" \
  -H "Authorization: Bearer $KEY"

# 提取最新一封邮件 OTP
curl -s "$BASE/api/mailboxes/<mailbox-id>/otp/latest" \
  -H "Authorization: Bearer $KEY"

# 收藏 / 取消收藏邮箱
curl -s -X PUT "$BASE/api/mailboxes/<mailbox-id>/favorite" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"favorite":true}'
```

域名接口：

```bash
# 查看共享域名池（支持过滤）
curl -s "$BASE/api/domains?status=active&hostname=mail.example.com&q=example" \
  -H "Authorization: Bearer $KEY"

# 普通用户提交域名验证
curl -s -X POST "$BASE/api/domains/submit" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com"}'

# 查询域名状态
curl -s "$BASE/api/domains/<domain-id>/status" \
  -H "Authorization: Bearer $KEY"
```

管理员域名接口：

```bash
# 手动添加域名（可选 hostname）
curl -s -X POST "$BASE/api/admin/domains" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com","hostname":"mail.example.com"}'

# 更新域名 hostname
curl -s -X PUT "$BASE/api/admin/domains/<domain-id>/hostname" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"hostname":"mail.example.com"}'

# MX 导入（可 force）
curl -s -X POST "$BASE/api/admin/domains/mx-import" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com","hostname":"mail.example.com","force":false}'

# MX 自动注册
curl -s -X POST "$BASE/api/admin/domains/mx-register" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com","hostname":"mail.example.com"}'

# Cloudflare 自动创建 MX
curl -s -X POST "$BASE/api/admin/domains/cf-create" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"domain":"sub.example.com","hostname":"mail.example.com"}'

# Cloudflare 删除 MX 并删除本地域名
curl -s -X DELETE "$BASE/api/admin/domains/<domain-id>/cf" \
  -H "Authorization: Bearer $KEY"

# 批量启停
curl -s -X PUT "$BASE/api/admin/domains/batch/toggle" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"ids":[1,2,3],"active":true}'

# 批量删除（可选联动删除 Cloudflare）
curl -s -X PUT "$BASE/api/admin/domains/batch/delete" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"ids":[1,2,3],"delete_cloudflare":true}'
```

---

## 系统设置

管理员后台支持以下重要设置：

- `registration_open`
- `smtp_server_ip`
- `smtp_hostname`
- `mailbox_ttl_minutes`
- `catchall_enabled`
- `catchall_account_id`
- `cf_api_token`
- `site_title`
- `announcement`
- `default_domain`
- `max_mailboxes_per_user`

说明：

- `cf_api_token` 需要具备 Cloudflare `Zone:DNS:Edit` 权限
- `smtp_server_ip` / `smtp_hostname` 的数据库设置优先于环境变量

---

## 数据库迁移

当前迁移文件：

| 文件 | 用途 |
|------|------|
| `sql/init.sql` | 新库全量初始化 |
| `sql/migrate_v2.sql` | 添加 `mailboxes.expires_at` |
| `sql/migrate_v3.sql` | 添加 `domains.status`、`domains.mx_checked_at` 和更多设置项 |
| `sql/migrate_v4.sql` | 添加 `mailboxes.is_favorite`、catch-all 设置项 |
| `sql/migrate_v5.sql` | 添加 `domains.hostname`、`cf_api_token` |

当前 API 在启动时也会自动补齐缺失的兼容字段和设置项，所以旧库重启后端后通常能自愈升级；但如果你希望显式执行迁移，也可以手工跑：

```bash
docker exec -i $(docker compose ps -q postgres) \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" < sql/migrate_v5.sql
```

---

## 项目结构

```text
tempmail/
├── api/
│   ├── cf/              # Cloudflare API 客户端
│   ├── config/          # 环境变量配置
│   ├── handler/         # HTTP 处理器
│   ├── middleware/      # 鉴权、限流
│   ├── model/           # 数据结构
│   ├── otp/             # OTP 提取逻辑
│   ├── store/           # PostgreSQL 访问层
│   └── main.go
├── frontend/
│   ├── css/style.css
│   ├── js/app.js
│   └── index.html
├── nginx/
├── postfix/
├── pgbouncer/
├── sql/
├── docker-compose.yml
└── README.md
```

---

## 后台任务

| 任务 | 间隔 | 说明 |
|------|------|------|
| 邮箱清理器 | 1 分钟 | 删除过期且未收藏的邮箱 |
| Pending 域名验证器 | 30 秒 | 激活 MX 已生效的待验证域名 |
| Active 域名健康巡检 | 6 小时 | 重新检查已激活域名的 MX 健康 |
| Admin Key 写入 | 启动后一次 | 把管理员 Key 写入 `data/admin.key` |

---

## 验证建议

升级后建议优先验证：

1. 登录后台，确认系统设置里可见 `catch-all` 和 `Cloudflare Token`
2. 创建邮箱并测试三栏视图、收藏、前端取码
3. 调用 `GET /api/mailboxes/:id/otp/latest`
4. 添加带 `hostname` 的域名，检查 DNS 提示是否正确
5. 测试域名筛选和批量操作
6. 若已配置 `cf_api_token`，测试 `cf-create` 和 `:id/cf`
7. 测试 catch-all 自动建箱是否仍正常

---

## 许可证

MIT
