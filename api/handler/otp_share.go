package handler

import (
	"fmt"
	"log"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tempmail/mailutil"
	"tempmail/middleware"
	"tempmail/model"
	"tempmail/store"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type OTPShareHandler struct {
	store *store.Store
}

var (
	otpShareAPIKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{15,95}$`)
)

func NewOTPShareHandler(s *store.Store) *OTPShareHandler {
	return &OTPShareHandler{store: s}
}

// GET /api/otp-shares - 列出当前账号名下的所有邮箱级 OTP 分享。
func (h *OTPShareHandler) List(c *gin.Context) {
	account := middleware.GetAccount(c)
	shares, err := h.store.ListMailboxOTPShares(c.Request.Context(), account.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	items := make([]gin.H, 0, len(shares))
	for i := range shares {
		items = append(items, buildOTPShareResponse(c, &shares[i]))
	}
	c.JSON(http.StatusOK, gin.H{"shares": items, "total": len(items)})
}

// GET /api/mailboxes/:id/otp-share - 查看当前邮箱的 OTP 分享。
func (h *OTPShareHandler) Get(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	share, err := h.store.GetMailboxOTPShare(c.Request.Context(), mailboxID, account.ID)
	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "otp share not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"share": buildOTPShareResponse(c, share)})
}

// POST /api/mailboxes/:id/otp-share - 创建、编辑或轮换邮箱级 OTP 分享。
func (h *OTPShareHandler) Upsert(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	var req struct {
		APIKey       string `json:"api_key"`
		Enabled      *bool  `json:"enabled"`
		ExpiresDays  *int   `json:"expires_days"`
		RotateAPIKey bool   `json:"rotate_api_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !strings.Contains(err.Error(), "EOF") {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := h.store.GetMailboxOTPShare(c.Request.Context(), mailboxID, account.ID)
	if err != nil && err != pgx.ErrNoRows {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	apiKey, enabled := "", true
	var expiresAt *time.Time
	if existing != nil {
		apiKey = existing.APIKey
		enabled = existing.Enabled
		expiresAt = existing.ExpiresAt
	}
	if req.RotateAPIKey {
		apiKey = ""
	}
	if strings.TrimSpace(req.APIKey) != "" {
		apiKey, err = normalizeOTPShareAPIKey(req.APIKey)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.ExpiresDays != nil {
		expiresAt, err = otpShareExpiry(*req.ExpiresDays, time.Now())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	share, err := h.store.UpsertMailboxOTPShare(
		c.Request.Context(), mailboxID, account.ID, apiKey, enabled, expiresAt,
	)
	if err != nil {
		switch err {
		case pgx.ErrNoRows:
			c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		case store.ErrMailboxOTPShareAPIKeyConflict:
			c.JSON(http.StatusConflict, gin.H{"error": "share api key already in use"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"share": buildOTPShareResponse(c, share)})
}

// DELETE /api/mailboxes/:id/otp-share - 永久收回当前邮箱的 OTP 分享。
func (h *OTPShareHandler) Delete(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	if err := h.store.DeleteMailboxOTPShare(c.Request.Context(), mailboxID, account.ID); err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "otp share not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "otp share deleted"})
}

func (h *OTPShareHandler) APIKeyLatest(c *gin.Context) {
	share := h.requireAPIKeyShare(c)
	if share != nil {
		h.respondLatest(c, share)
	}
}

func (h *OTPShareHandler) APIKeyEmails(c *gin.Context) {
	share := h.requireAPIKeyShare(c)
	if share != nil {
		h.respondEmails(c, share)
	}
}

func (h *OTPShareHandler) APIKeyEmail(c *gin.Context) {
	share := h.requireAPIKeyShare(c)
	if share != nil {
		h.respondEmail(c, share)
	}
}

func (h *OTPShareHandler) APIKeyEmailOTP(c *gin.Context) {
	share := h.requireAPIKeyShare(c)
	if share != nil {
		h.respondEmailOTP(c, share)
	}
}

func (h *OTPShareHandler) APIKeyAttachment(c *gin.Context) {
	share := h.requireAPIKeyShare(c)
	if share != nil {
		h.respondAttachment(c, share)
	}
}

func (h *OTPShareHandler) PageMailbox(c *gin.Context) {
	share := h.requirePageShare(c)
	if share == nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"mailbox":    gin.H{"full_address": share.FullAddress},
		"expires_at": share.ExpiresAt,
	})
}

func (h *OTPShareHandler) PageLatest(c *gin.Context) {
	share := h.requirePageShare(c)
	if share != nil {
		h.respondLatest(c, share)
	}
}

func (h *OTPShareHandler) PageEmails(c *gin.Context) {
	share := h.requirePageShare(c)
	if share != nil {
		h.respondEmails(c, share)
	}
}

func (h *OTPShareHandler) PageEmail(c *gin.Context) {
	share := h.requirePageShare(c)
	if share != nil {
		h.respondEmail(c, share)
	}
}

func (h *OTPShareHandler) PageEmailOTP(c *gin.Context) {
	share := h.requirePageShare(c)
	if share != nil {
		h.respondEmailOTP(c, share)
	}
}

func (h *OTPShareHandler) PageAttachment(c *gin.Context) {
	share := h.requirePageShare(c)
	if share != nil {
		h.respondAttachment(c, share)
	}
}

func (h *OTPShareHandler) requireAPIKeyShare(c *gin.Context) *model.MailboxOTPShare {
	apiKey := extractOTPShareAPIKey(c)
	if apiKey == "" {
		h.respondOTPErr(c, http.StatusUnauthorized, "missing share api key")
		return nil
	}
	share, err := h.store.GetMailboxOTPShareByAPIKey(c.Request.Context(), apiKey)
	if err != nil {
		if err == pgx.ErrNoRows {
			h.respondOTPErr(c, http.StatusUnauthorized, "invalid, stopped or expired share api key")
			return nil
		}
		h.respondOTPErr(c, http.StatusInternalServerError, err.Error())
		return nil
	}
	return share
}

func (h *OTPShareHandler) requirePageShare(c *gin.Context) *model.MailboxOTPShare {
	share, err := h.store.GetMailboxOTPShareByAPIKey(c.Request.Context(), strings.TrimSpace(c.Param("api_key")))
	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "share not found, stopped or expired"})
			return nil
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil
	}
	return share
}

func (h *OTPShareHandler) respondLatest(c *gin.Context, share *model.MailboxOTPShare) {
	email, err := h.store.GetLatestEmail(c.Request.Context(), share.MailboxID)
	if err != nil {
		if err == pgx.ErrNoRows {
			h.respondOTPErr(c, http.StatusNotFound, "no emails found")
			return
		}
		h.respondOTPErr(c, http.StatusInternalServerError, err.Error())
		return
	}

	code := extractLatestOTPCode(email, loadOTPExtractionConfig(c.Request.Context(), h.store))
	if code == "" {
		h.respondOTPErr(c, http.StatusUnprocessableEntity, "otp not found in latest email")
		return
	}

	if strings.EqualFold(strings.TrimSpace(c.Query("format")), "text") {
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(code+"\n"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"otp": buildLatestOTPResponse(share, email, code)})
}

func (h *OTPShareHandler) respondEmails(c *gin.Context, share *model.MailboxOTPShare) {
	emails, total, err := h.store.ListEmails(c.Request.Context(), share.MailboxID, 1, 5)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"emails": emails, "total": total})
}

func (h *OTPShareHandler) respondEmail(c *gin.Context, share *model.MailboxOTPShare) {
	email := h.getSharedEmail(c, share)
	if email == nil {
		return
	}
	email.Recipient = mailutil.OriginalRecipient(email.RawMessage)
	if email.Recipient == "" {
		email.Recipient = share.FullAddress
	}
	attachments, renderedHTML, err := mailutil.ParseAttachmentsAndInlineHTML(email.RawMessage, email.BodyHTML)
	if err != nil {
		log.Printf("[otp-share] parse email %s failed: %v", email.ID, err)
	} else {
		email.Attachments = mailutil.Meta(attachments)
		email.BodyHTML = renderedHTML
	}
	email.RawMessage = ""
	c.JSON(http.StatusOK, gin.H{"email": email})
}

func (h *OTPShareHandler) respondEmailOTP(c *gin.Context, share *model.MailboxOTPShare) {
	email := h.getSharedEmail(c, share)
	if email == nil {
		return
	}
	code := extractLatestOTPCode(email, loadOTPExtractionConfig(c.Request.Context(), h.store))
	if code == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "otp not found in email"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"otp": buildLatestOTPResponse(share, email, code)})
}

func (h *OTPShareHandler) respondAttachment(c *gin.Context, share *model.MailboxOTPShare) {
	email := h.getSharedEmail(c, share)
	if email == nil {
		return
	}
	attachmentID, err := strconv.Atoi(c.Param("attachment_id"))
	if err != nil || attachmentID < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid attachment id"})
		return
	}
	attachments, err := mailutil.ParseAttachments(email.RawMessage)
	if err != nil {
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
	c.Header("Content-Disposition", disposition)
	c.Header("Content-Length", strconv.Itoa(len(attachment.Data)))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, contentType, attachment.Data)
}

func (h *OTPShareHandler) getSharedEmail(c *gin.Context, share *model.MailboxOTPShare) *model.Email {
	emailID, err := parseUUID(c.Param("email_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email id"})
		return nil
	}
	email, err := h.store.GetEmail(c.Request.Context(), emailID, share.MailboxID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return nil
	}
	return email
}

func buildLatestOTPResponse(share *model.MailboxOTPShare, email *model.Email, code string) model.LatestOTPResponse {
	return model.LatestOTPResponse{
		MailboxID:   share.MailboxID,
		FullAddress: share.FullAddress,
		EmailID:     email.ID,
		Code:        code,
		Subject:     email.Subject,
		Sender:      email.Sender,
		ReceivedAt:  email.ReceivedAt,
	}
}

func (h *OTPShareHandler) respondOTPErr(c *gin.Context, status int, msg string) {
	if strings.EqualFold(strings.TrimSpace(c.Query("format")), "text") {
		c.Data(status, "text/plain; charset=utf-8", []byte(msg+"\n"))
		return
	}
	c.JSON(status, gin.H{"error": msg})
}

func buildOTPShareResponse(c *gin.Context, share *model.MailboxOTPShare) gin.H {
	origin := fmt.Sprintf("%s://%s", detectRequestScheme(c), c.Request.Host)
	pageURL := fmt.Sprintf("%s/otp-share/%s", origin, share.APIKey)
	latestAPI := origin + "/public/otp-share/latest"
	emailsAPI := origin + "/public/otp-share/emails"
	return gin.H{
		"mailbox_id":     share.MailboxID,
		"full_address":   share.FullAddress,
		"api_key":        share.APIKey,
		"enabled":        share.Enabled,
		"expired":        share.ExpiresAt != nil && !share.ExpiresAt.After(time.Now()),
		"expires_at":     share.ExpiresAt,
		"url":            pageURL,
		"api_url":        latestAPI,
		"emails_api_url": emailsAPI,
		"curl":           fmt.Sprintf("curl -fsSL -H \"Authorization: Bearer %s\" '%s?format=text'", share.APIKey, latestAPI),
		"emails_curl":    fmt.Sprintf("curl -fsSL -H \"Authorization: Bearer %s\" '%s'", share.APIKey, emailsAPI),
		"created_at":     share.CreatedAt,
		"updated_at":     share.UpdatedAt,
	}
}

func extractOTPShareAPIKey(c *gin.Context) string {
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(authorization) >= 7 && strings.EqualFold(authorization[:7], "Bearer ") {
		return strings.TrimSpace(authorization[7:])
	}
	return strings.TrimSpace(c.Query("api_key"))
}

func normalizeOTPShareAPIKey(raw string) (string, error) {
	apiKey := strings.TrimSpace(raw)
	if apiKey == "" {
		return "", nil
	}
	if !otpShareAPIKeyPattern.MatchString(apiKey) {
		return "", fmt.Errorf("invalid api key: use 16-96 chars of letters, numbers, _ or -")
	}
	return apiKey, nil
}

func otpShareExpiry(days int, now time.Time) (*time.Time, error) {
	if days < 0 || days > 3650 {
		return nil, fmt.Errorf("expires_days must be between 0 and 3650")
	}
	if days == 0 {
		return nil, nil
	}
	expiresAt := now.Add(time.Duration(days) * 24 * time.Hour)
	return &expiresAt, nil
}

func detectRequestScheme(c *gin.Context) string {
	if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto != "" {
		return strings.ToLower(strings.Split(proto, ",")[0])
	}
	if c.Request.TLS != nil {
		return "https"
	}
	return "http"
}
