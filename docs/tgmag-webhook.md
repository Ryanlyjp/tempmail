# tgmag 持久化 Webhook

TempMail 可把指定域名收到的邮件推送到 tgmag。该通道只负责可靠传输，不在 TempMail 内判断 OTP；tgmag 会按完整收件地址、请求时间、Telegram 发件人、Login 用途和验证码一致性过滤。

## 配置

老数据库先执行：

```bash
psql "$DATABASE_URL" -f sql/migrate_v13.sql
```

也可以直接重启 API，由启动兼容逻辑创建同一张表和默认设置。

进入管理员“系统设置 → tgmag Webhook”，只需配置：

1. 接收地址：tgmag 的 `https://<host>/webhooks/selfhosted-tempmail`。
2. 签名密钥：至少 32 个非空白字符，并与 tgmag 的 `LOGIN_EMAIL_SELFHOSTED_WEBHOOK_SECRET` 一致。
3. 域名：从已有域名下拉框中添加参与 Webhook 的域名；已添加项才会显示，按需勾选“包含子域名”。
4. 启用持久化 Webhook并保存。

URL 仅允许 HTTP/HTTPS，不能包含账号密码、查询参数或 fragment。生产环境应使用 HTTPS；HTTP 仅适合可信内网。

## 投递语义

- 邮件和 outbox 在同一个 PostgreSQL 事务中写入。
- outbox 保存完整邮件内容且不引用短期邮箱；邮箱 TTL 清理不会删除待投递邮件。
- 2xx 表示成功，其他状态或网络错误按固定退避重试。
- 每次请求固定 10 秒超时，不跟随重定向。
- 待投递记录最多保留 24 小时。
- 可能发生重复投递，tgmag 使用邮件 ID 和完整收件地址幂等去重。

签名头为：

```text
X-TempMail-Delivery: <outbox UUID>
X-TempMail-Timestamp: <Unix seconds>
X-TempMail-Signature: <hex HMAC-SHA256>
```

签名输入是 `timestamp + "." + raw JSON body`。修改 URL、密钥或域名规则只影响之后的投递尝试；关闭开关会暂停 outbox 投递，重新开启后继续。

## 回退

先在设置页关闭 Webhook。代码回退可恢复本次变更文件；`webhook_outbox` 和四项 `tgmag_webhook_*` 设置可以保留，不影响旧版本运行。确认不再需要历史待投递数据后，才手工删除该表和设置。
