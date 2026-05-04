package handler

import (
	"context"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tempmail/mailutil"
	"tempmail/middleware"
	"tempmail/model"
	"tempmail/otp"
	"tempmail/store"
	"tempmail/telegrambot"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type EmailHandler struct {
	store *store.Store
}

func NewEmailHandler(s *store.Store) *EmailHandler {
	return &EmailHandler{store: s}
}

// GET /api/mailboxes/:mailbox_id/emails - 列出邮件
func (h *EmailHandler) List(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	// 验证邮箱归属
	_, err = h.store.GetMailbox(c.Request.Context(), mailboxID, account.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	emails, total, err := h.store.ListEmails(c.Request.Context(), mailboxID, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  emails,
		"total": total,
		"page":  page,
		"size":  size,
	})
}

// GET /api/mailboxes/:mailbox_id/emails/:id - 读取邮件内容
func (h *EmailHandler) Get(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	// 验证邮箱归属
	_, err = h.store.GetMailbox(c.Request.Context(), mailboxID, account.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}

	emailID, err := parseUUID(c.Param("email_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email id"})
		return
	}

	email, err := h.store.GetEmail(c.Request.Context(), emailID, mailboxID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}

	attachments, err := mailutil.ParseAttachments(email.RawMessage)
	if err != nil {
		log.Printf("[attachments] parse email %s failed: %v", email.ID, err)
	} else {
		email.Attachments = mailutil.Meta(attachments)
	}

	c.JSON(http.StatusOK, gin.H{"email": email})
}

// GET /api/mailboxes/:id/emails/:email_id/attachments/:attachment_id - 下载附件
func (h *EmailHandler) DownloadAttachment(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	// 验证邮箱归属
	_, err = h.store.GetMailbox(c.Request.Context(), mailboxID, account.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}

	emailID, err := parseUUID(c.Param("email_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email id"})
		return
	}

	email, err := h.store.GetEmail(c.Request.Context(), emailID, mailboxID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}

	attachmentID, err := strconv.Atoi(c.Param("attachment_id"))
	if err != nil || attachmentID < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid attachment id"})
		return
	}

	attachments, err := mailutil.ParseAttachments(email.RawMessage)
	if err != nil {
		log.Printf("[attachments] parse email %s failed: %v", email.ID, err)
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "attachments unavailable for this email"})
		return
	}

	attachment := mailutil.Find(attachments, attachmentID)
	if attachment == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "attachment not found"})
		return
	}

	contentType := attachment.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Filename})
	if disposition == "" {
		disposition = `attachment; filename="download.bin"`
	}

	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", disposition)
	c.Header("Content-Length", strconv.Itoa(len(attachment.Data)))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, contentType, attachment.Data)
}

// POST /api/mailboxes/:id/emails/:email_id/forward/tg - 手动转发已有邮件到 TG
func (h *EmailHandler) ForwardTelegram(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	mailbox, err := h.store.GetMailbox(c.Request.Context(), mailboxID, account.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}

	emailID, err := parseUUID(c.Param("email_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email id"})
		return
	}

	email, err := h.store.GetEmail(c.Request.Context(), emailID, mailboxID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	cfg, err := telegrambot.LoadConfig(ctx, h.store)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load telegram config"})
		return
	}
	if !telegrambot.ConfigReady(cfg) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "telegram not configured"})
		return
	}

	attachments, err := mailutil.ParseAttachments(email.RawMessage)
	if err != nil {
		log.Printf("[tg-forward] manual parse email %s failed: %v", email.ID, err)
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "attachments unavailable for this email"})
		return
	}

	if err := telegrambot.SendEmailWithMode(ctx, cfg, *mailbox, *email, attachments, telegrambot.ModeAllWithAttachments); err != nil {
		log.Printf("[tg-forward] manual send failed for %s: %v", mailbox.FullAddress, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "forwarded to telegram",
		"attachments_sent": len(attachments),
	})
}

// GET /api/mailboxes/:id/otp/latest - 提取最新一封邮件中的 OTP
func (h *EmailHandler) LatestOTP(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	mailbox, err := h.store.GetMailbox(c.Request.Context(), mailboxID, account.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}

	email, err := h.store.GetLatestEmail(c.Request.Context(), mailboxID)
	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "no emails found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	code := otp.ExtractFromHTML(email.BodyHTML)
	if code == "" {
		text := strings.Join([]string{email.BodyText, otp.StripHTML(email.BodyHTML), email.Subject}, "\n")
		code = otp.Extract(text)
	}
	if code == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "otp not found in latest email"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"otp": model.LatestOTPResponse{
			MailboxID:   mailbox.ID,
			FullAddress: mailbox.FullAddress,
			EmailID:     email.ID,
			Code:        code,
			Subject:     email.Subject,
			Sender:      email.Sender,
			ReceivedAt:  email.ReceivedAt,
		},
	})
}

// DELETE /api/mailboxes/:mailbox_id/emails/:id - 删除邮件
func (h *EmailHandler) Delete(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	// 验证邮箱归属
	_, err = h.store.GetMailbox(c.Request.Context(), mailboxID, account.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}

	emailID, err := parseUUID(c.Param("email_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email id"})
		return
	}

	if err := h.store.DeleteEmail(c.Request.Context(), emailID, mailboxID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "email deleted"})
}
