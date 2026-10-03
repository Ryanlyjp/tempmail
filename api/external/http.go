package external

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tempmail/mailutil"
	"tempmail/model"
	"tempmail/otp"
)

func fail(c *gin.Context, status int, msg string) { c.AbortWithStatusJSON(status, gin.H{"error": msg}) }
func validID(c *gin.Context, name string) bool {
	if _, err := uuid.Parse(c.Param(name)); err != nil {
		fail(c, 400, "invalid "+name)
		return false
	}
	return true
}
func (s *Service) Register(admin, public *gin.RouterGroup) {
	g := admin.Group("/external")
	g.GET("/accounts", func(c *gin.Context) {
		a, err := s.accounts(c.Request.Context())
		if err != nil {
			fail(c, 500, "读取接入账号失败")
			return
		}
		c.JSON(200, gin.H{"accounts": a})
	})
	g.POST("/accounts", s.saveAccount)
	g.PUT("/accounts/:account_id", s.saveAccount)
	g.PUT("/accounts/:account_id/tg", func(c *gin.Context) {
		if !validID(c, "account_id") {
			return
		}
		var req struct {
			TG bool `json:"tg_enabled"`
		}
		if c.ShouldBindJSON(&req) != nil {
			fail(c, 400, "invalid request")
			return
		}
		tag, err := s.db.Exec(c.Request.Context(), `UPDATE external_accounts SET tg_enabled=$2 WHERE id=$1`, c.Param("account_id"), req.TG)
		if err != nil {
			fail(c, 500, "更新失败")
			return
		}
		if tag.RowsAffected() == 0 {
			fail(c, 404, "账号不存在")
			return
		}
		c.JSON(200, gin.H{"message": "提醒设置已更新"})
	})
	g.DELETE("/accounts/:account_id", func(c *gin.Context) {
		if !validID(c, "account_id") {
			return
		}
		tag, err := s.db.Exec(c.Request.Context(), `DELETE FROM external_accounts WHERE id=$1`, c.Param("account_id"))
		if err != nil {
			fail(c, 500, "删除失败")
			return
		}
		if tag.RowsAffected() == 0 {
			fail(c, 404, "账号不存在")
			return
		}
		c.JSON(200, gin.H{"message": "外部邮箱已删除"})
	})
	g.POST("/test", s.testAccount)
	g.POST("/accounts/:account_id/sync", func(c *gin.Context) {
		if !validID(c, "account_id") {
			return
		}
		tag, err := s.db.Exec(c.Request.Context(), `UPDATE external_accounts SET requested=true WHERE id=$1 AND enabled`, c.Param("account_id"))
		if err != nil {
			fail(c, 500, "提交刷新失败")
			return
		}
		if tag.RowsAffected() == 0 {
			fail(c, 409, "账号不存在或已暂停，请先启用同步")
			return
		}
		c.JSON(202, gin.H{"message": "已加入同步队列"})
	})
	g.GET("/accounts/:account_id/mailboxes", func(c *gin.Context) {
		if !validID(c, "account_id") {
			return
		}
		b, err := s.mailboxes(c.Request.Context(), c.Param("account_id"))
		if err != nil {
			fail(c, 500, "读取子邮箱失败")
			return
		}
		c.JSON(200, gin.H{"mailboxes": b})
	})
	g.POST("/accounts/:account_id/mailboxes", func(c *gin.Context) {
		if !validID(c, "account_id") {
			return
		}
		var req struct {
			Address string `json:"address"`
		}
		if c.ShouldBindJSON(&req) != nil || address(req.Address) == "" {
			fail(c, 400, "请输入有效邮箱地址")
			return
		}
		if err := s.ensureMailbox(c.Request.Context(), c.Param("account_id"), address(req.Address)); err != nil {
			fail(c, 400, "新增子邮箱失败")
			return
		}
		c.JSON(200, gin.H{"message": "子邮箱已添加"})
	})
	g.PUT("/mailboxes/:mailbox_id", func(c *gin.Context) {
		if !validID(c, "mailbox_id") {
			return
		}
		var req struct {
			TG bool `json:"tg_enabled"`
		}
		if c.ShouldBindJSON(&req) != nil {
			fail(c, 400, "invalid request")
			return
		}
		tag, err := s.db.Exec(c.Request.Context(), `UPDATE external_mailboxes SET tg_enabled=$2 WHERE id=$1`, c.Param("mailbox_id"), req.TG)
		if err != nil {
			fail(c, 500, "更新失败")
			return
		}
		if tag.RowsAffected() == 0 {
			fail(c, 404, "子邮箱不存在")
			return
		}
		c.JSON(200, gin.H{"message": "提醒设置已更新"})
	})
	g.DELETE("/mailboxes/:mailbox_id", func(c *gin.Context) {
		if !validID(c, "mailbox_id") {
			return
		}
		tag, err := s.db.Exec(c.Request.Context(), `DELETE FROM external_mailboxes WHERE id=$1`, c.Param("mailbox_id"))
		if err != nil {
			fail(c, 500, "删除失败")
			return
		}
		if tag.RowsAffected() == 0 {
			fail(c, 404, "子邮箱不存在")
			return
		}
		c.JSON(200, gin.H{"message": "子邮箱记录已删除"})
	})
	g.GET("/shares", s.listShares)
	g.PUT("/mailboxes/:mailbox_id/share", s.saveShare)
	g.DELETE("/mailboxes/:mailbox_id/share", func(c *gin.Context) {
		if !validID(c, "mailbox_id") {
			return
		}
		_, err := s.db.Exec(c.Request.Context(), `DELETE FROM external_shares WHERE mailbox_id=$1`, c.Param("mailbox_id"))
		if err != nil {
			fail(c, 500, "收回失败")
			return
		}
		c.JSON(200, gin.H{"message": "分享已收回"})
	})
	for _, base := range []string{"/accounts/:account_id", "/mailboxes/:mailbox_id"} {
		g.GET(base+"/emails", s.adminRead("list"))
		g.GET(base+"/emails/:email_id", s.adminRead("detail"))
		g.GET(base+"/emails/:email_id/otp", s.adminRead("otp"))
		g.GET(base+"/emails/:email_id/attachments/:attachment_id", s.adminRead("attachment"))
	}
	g.GET("/mailboxes/:mailbox_id/otp/latest", s.adminRead("latest"))
	g.GET("/accounts/:account_id/otp/latest", s.adminRead("latest"))
	p := public.Group("/external-otp")
	for _, base := range []string{"", "/page/:api_key"} {
		p.GET(base+"/mailbox", s.sharedRead("mailbox"))
		p.GET(base+"/latest", s.sharedRead("latest"))
		p.GET(base+"/emails", s.sharedRead("list"))
		p.GET(base+"/emails/:email_id", s.sharedRead("detail"))
		p.GET(base+"/emails/:email_id/otp", s.sharedRead("otp"))
		p.GET(base+"/emails/:email_id/attachments/:attachment_id", s.sharedRead("attachment"))
	}
}
func (s *Service) saveAccount(c *gin.Context) {
	var a Account
	if c.ShouldBindJSON(&a) != nil {
		fail(c, 400, "invalid request")
		return
	}
	a.ID = c.Param("account_id")
	if a.ID != "" && !validID(c, "account_id") {
		return
	}
	saved, err := s.save(c.Request.Context(), a)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	c.JSON(200, gin.H{"account": saved})
}
func (s *Service) testAccount(c *gin.Context) {
	var a Account
	if c.ShouldBindJSON(&a) != nil {
		fail(c, 400, "invalid request")
		return
	}
	if err := normalize(&a); err != nil {
		fail(c, 400, err.Error())
		return
	}
	if a.ID != "" {
		if _, err := uuid.Parse(a.ID); err != nil {
			fail(c, 400, "invalid account id")
			return
		}
		old, err := s.account(c.Request.Context(), a.ID)
		if err != nil {
			fail(c, 404, "账号不存在")
			return
		}
		if a.Credentials.Password == "" {
			a.Credentials.Password = old.Credentials.Password
		}
		if a.Credentials.ClientID == "" {
			a.Credentials.ClientID = old.Credentials.ClientID
		}
		if a.Credentials.RefreshToken == "" {
			a.Credentials.RefreshToken = old.Credentials.RefreshToken
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	var err error
	if a.Provider == "outlook" && a.Protocol != "imap_oauth" {
		var token string
		token, err = s.oauth(ctx, &a, "https://graph.microsoft.com/.default offline_access")
		if err == nil {
			_, err = graphGet(ctx, token, "https://graph.microsoft.com/v1.0/me/messages?$top=1&$select=id,body")
		}
		if err != nil && a.Protocol == "auto" {
			err = s.imapSync(ctx, a, "", true, true)
		}
	} else {
		err = s.imapSync(ctx, a, "", true, true)
	}
	if err != nil {
		fail(c, 502, err.Error())
		return
	}
	c.JSON(200, gin.H{"message": "连接和读取权限测试通过"})
}
func newKey(addr string) (string, error) {
	name, _, _ := strings.Cut(addr, "@")
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return name + "_" + hex.EncodeToString(b), nil
}
func (s *Service) listShares(c *gin.Context) {
	rows, err := s.db.Query(c.Request.Context(), `SELECT s.mailbox_id,b.address,s.api_key,s.enabled,s.expires_at FROM external_shares s JOIN external_mailboxes b ON b.id=s.mailbox_id ORDER BY s.updated_at DESC`)
	if err != nil {
		fail(c, 500, "读取分享失败")
		return
	}
	defer rows.Close()
	out := []Share{}
	for rows.Next() {
		var v Share
		if err = rows.Scan(&v.MailboxID, &v.Address, &v.APIKey, &v.Enabled, &v.ExpiresAt); err != nil {
			fail(c, 500, "读取分享失败")
			return
		}
		out = append(out, v)
	}
	if rows.Err() != nil {
		fail(c, 500, "读取分享失败")
		return
	}
	c.JSON(200, gin.H{"shares": out})
}
func (s *Service) saveShare(c *gin.Context) {
	if !validID(c, "mailbox_id") {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
		Days    *int `json:"expires_days"`
		Rotate  bool `json:"rotate"`
	}
	if c.ShouldBindJSON(&req) != nil {
		fail(c, 400, "invalid request")
		return
	}
	b, err := s.mailbox(c.Request.Context(), c.Param("mailbox_id"))
	if err != nil {
		fail(c, 404, "子邮箱不存在")
		return
	}
	var key string
	var expires *time.Time
	err = s.db.QueryRow(c.Request.Context(), `SELECT api_key,expires_at FROM external_shares WHERE mailbox_id=$1`, b.ID).Scan(&key, &expires)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		fail(c, 500, "读取分享失败")
		return
	}
	if key == "" || req.Rotate {
		key, err = newKey(b.Address)
		if err != nil {
			fail(c, 500, "生成 Key 失败")
			return
		}
	}
	if req.Days != nil {
		if *req.Days < 0 || *req.Days > 3650 {
			fail(c, 400, "有效期须为 0 到 3650 天，0 表示永久")
			return
		}
		expires = nil
		if *req.Days > 0 {
			t := time.Now().Add(time.Duration(*req.Days) * 24 * time.Hour)
			expires = &t
		}
	}
	_, err = s.db.Exec(c.Request.Context(), `INSERT INTO external_shares(mailbox_id,api_key,enabled,expires_at) VALUES($1,$2,$3,$4) ON CONFLICT(mailbox_id) DO UPDATE SET api_key=$2,enabled=$3,expires_at=$4,updated_at=NOW()`, b.ID, key, req.Enabled, expires)
	if err != nil {
		fail(c, 500, "保存分享失败")
		return
	}
	c.JSON(200, gin.H{"share": Share{b.ID, b.Address, key, req.Enabled, expires}})
}
func (s *Service) adminRead(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		var b Mailbox
		var err error
		if c.Param("mailbox_id") != "" {
			if !validID(c, "mailbox_id") {
				return
			}
			b, err = s.mailbox(c.Request.Context(), c.Param("mailbox_id"))
			if err != nil {
				fail(c, 404, "子邮箱不存在")
				return
			}
		} else {
			if !validID(c, "account_id") {
				return
			}
			b.AccountID = c.Param("account_id")
		}
		s.read(c, b, kind, false, nil)
	}
}
func (s *Service) sharedRead(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		key := c.Param("api_key")
		if key == "" {
			key = strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
		}
		if key == "" {
			fail(c, 401, "missing share API key")
			return
		}
		var b Mailbox
		var expires *time.Time
		err := s.db.QueryRow(c.Request.Context(), `SELECT b.id,b.account_id,b.address,s.expires_at FROM external_shares s JOIN external_mailboxes b ON b.id=s.mailbox_id WHERE s.api_key=$1 AND s.enabled AND (s.expires_at IS NULL OR s.expires_at>NOW())`, key).Scan(&b.ID, &b.AccountID, &b.Address, &expires)
		if err != nil || b.Address == "" {
			fail(c, 401, "invalid, stopped or expired share API key")
			return
		}
		s.read(c, b, kind, true, expires)
	}
}
func (s *Service) read(c *gin.Context, b Mailbox, kind string, shared bool, expires *time.Time) {
	ctx := c.Request.Context()
	filter := b.Address
	if kind == "mailbox" {
		var last *time.Time
		_ = s.db.QueryRow(ctx, `SELECT last_sync FROM external_accounts WHERE id=$1`, b.AccountID).Scan(&last)
		c.JSON(200, gin.H{"mailbox": gin.H{"id": b.ID, "full_address": b.Address}, "expires_at": expires, "last_sync": last})
		return
	}
	if kind == "list" {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		if page > 100000 {
			page = 100000
		}
		size := 20
		if shared {
			page = 1
			size = 5
		}
		var total int
		err := s.db.QueryRow(ctx, `SELECT count(*) FROM external_messages WHERE account_id=$1 AND ($2='' OR recipient=$2) AND active`, b.AccountID, filter).Scan(&total)
		if err != nil {
			fail(c, 500, "读取邮件失败")
			return
		}
		rows, err := s.db.Query(ctx, `SELECT id,message,recipient,promotion FROM external_messages WHERE account_id=$1 AND ($2='' OR recipient=$2) AND active ORDER BY received_at DESC,id DESC LIMIT $3 OFFSET $4`, b.AccountID, filter, size, (page-1)*size)
		if err != nil {
			fail(c, 500, "读取邮件失败")
			return
		}
		defer rows.Close()
		list := []gin.H{}
		for rows.Next() {
			var id, recipient string
			var data []byte
			var promo bool
			if rows.Scan(&id, &data, &recipient, &promo) != nil {
				fail(c, 500, "读取邮件失败")
				return
			}
			var e model.Email
			if json.Unmarshal(data, &e) != nil {
				fail(c, 500, "读取邮件失败")
				return
			}
			list = append(list, gin.H{"id": id, "subject": e.Subject, "sender": e.Sender, "received_at": e.ReceivedAt, "recipient": recipient, "promotion": promo})
		}
		if rows.Err() != nil {
			fail(c, 500, "读取邮件失败")
			return
		}
		c.JSON(200, gin.H{"emails": list, "total": total, "page": page, "size": size})
		return
	}
	var data, raw []byte
	var id, recipient string
	var err error
	if kind == "latest" {
		err = s.db.QueryRow(ctx, `SELECT id,message,raw,recipient FROM external_messages WHERE account_id=$1 AND ($2='' OR recipient=$2) AND active ORDER BY received_at DESC,id DESC LIMIT 1`, b.AccountID, filter).Scan(&id, &data, &raw, &recipient)
	} else {
		if !validID(c, "email_id") {
			return
		}
		err = s.db.QueryRow(ctx, `SELECT id,message,raw,recipient FROM external_messages WHERE id=$1 AND account_id=$2 AND ($3='' OR recipient=$3) AND active`, c.Param("email_id"), b.AccountID, filter).Scan(&id, &data, &raw, &recipient)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fail(c, 404, "email not found")
		} else {
			fail(c, 500, "读取邮件失败")
		}
		return
	}
	var e model.Email
	if json.Unmarshal(data, &e) != nil {
		fail(c, 500, "邮件格式错误")
		return
	}
	e.ID, _ = uuid.Parse(id)
	e.Recipient = recipient
	if kind == "latest" || kind == "otp" {
		cfg := s.otpConfig(ctx)
		code := otp.ExtractFromHTMLWithConfig(e.BodyHTML, e.Sender, cfg)
		if code == "" {
			code = otp.ExtractWithConfig(e.BodyText+"\n"+otp.StripHTML(e.BodyHTML)+"\n"+e.Subject, e.Sender, cfg)
		}
		if code == "" {
			fail(c, 422, "otp not found in email")
			return
		}
		if c.Query("format") == "text" {
			c.Data(200, "text/plain; charset=utf-8", []byte(code+"\n"))
			return
		}
		c.JSON(200, gin.H{"otp": gin.H{"mailbox_id": b.ID, "full_address": recipient, "email_id": id, "code": code, "subject": e.Subject, "sender": e.Sender, "received_at": e.ReceivedAt}})
		return
	}
	if kind == "attachment" {
		n, err := strconv.Atoi(c.Param("attachment_id"))
		if err != nil || n < 1 {
			fail(c, 400, "invalid attachment id")
			return
		}
		assets, err := mailutil.ParseAttachments(string(raw))
		if err != nil {
			fail(c, 422, "附件解析失败")
			return
		}
		a := mailutil.Find(assets, n)
		if a == nil {
			fail(c, 404, "attachment not found")
			return
		}
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename}))
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(200, a.ContentType, a.Data)
		return
	}
	assets, html, err := mailutil.ParseAttachmentsAndInlineHTML(string(raw), e.BodyHTML)
	if err == nil {
		e.Attachments = mailutil.Meta(assets)
		e.BodyHTML = html
	}
	e.RawMessage = ""
	c.JSON(200, gin.H{"email": e})
}
func (s *Service) otpConfig(ctx context.Context) otp.SegmentedConfig {
	enabled, _ := s.settings.GetSetting(ctx, "otp_segmented_enabled")
	lengths, _ := s.settings.GetSetting(ctx, "otp_segmented_lengths")
	senders, _ := s.settings.GetSetting(ctx, "otp_segmented_senders")
	cfg := otp.SegmentedConfig{Enabled: enabled == "true"}
	for _, v := range strings.FieldsFunc(lengths, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' }) {
		if v == "3" {
			cfg.AllowThree = true
		}
		if v == "4" {
			cfg.AllowFour = true
		}
	}
	cfg.AllowedSenders = strings.FieldsFunc(senders, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' })
	return cfg
}
