package handler

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"tempmail/middleware"
	"tempmail/model"
	"tempmail/store"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type OTPShareHandler struct {
	store *store.Store
}

var otpShareTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{5,63}$`)

func NewOTPShareHandler(s *store.Store) *OTPShareHandler {
	return &OTPShareHandler{store: s}
}

// GET /api/mailboxes/:id/otp-share - 查看当前邮箱的 OTP 分享链接
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

// POST /api/mailboxes/:id/otp-share - 创建或轮换当前邮箱的 OTP 分享链接
func (h *OTPShareHandler) Upsert(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !strings.Contains(err.Error(), "EOF") {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, err := normalizeOTPShareToken(req.Token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	share, err := h.store.UpsertMailboxOTPShare(c.Request.Context(), mailboxID, account.ID, token)
	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
			return
		}
		if err == store.ErrMailboxOTPShareTokenConflict {
			c.JSON(http.StatusConflict, gin.H{"error": "share token already in use"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"share": buildOTPShareResponse(c, share)})
}

// DELETE /api/mailboxes/:id/otp-share - 关闭当前邮箱的 OTP 分享链接
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

// GET /public/otp-share/:token/latest - 公开读取某个邮箱最新 OTP
func (h *OTPShareHandler) PublicLatest(c *gin.Context) {
	token := extractPublicOTPShareToken(c)
	if token == "" {
		h.respondOTPErr(c, http.StatusBadRequest, "missing share token")
		return
	}

	share, err := h.store.GetMailboxOTPShareByToken(c.Request.Context(), token)
	if err != nil {
		if err == pgx.ErrNoRows {
			h.respondOTPErr(c, http.StatusNotFound, "otp share not found")
			return
		}
		h.respondOTPErr(c, http.StatusInternalServerError, err.Error())
		return
	}

	email, err := h.store.GetLatestEmail(c.Request.Context(), share.MailboxID)
	if err != nil {
		if err == pgx.ErrNoRows {
			h.respondOTPErr(c, http.StatusNotFound, "no emails found")
			return
		}
		h.respondOTPErr(c, http.StatusInternalServerError, err.Error())
		return
	}

	code := extractLatestOTPCode(email)
	if code == "" {
		h.respondOTPErr(c, http.StatusUnprocessableEntity, "otp not found in latest email")
		return
	}

	if strings.EqualFold(strings.TrimSpace(c.Query("format")), "text") {
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(code+"\n"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"otp": model.LatestOTPResponse{
			MailboxID:   share.MailboxID,
			FullAddress: share.FullAddress,
			EmailID:     email.ID,
			Code:        code,
			Subject:     email.Subject,
			Sender:      email.Sender,
			ReceivedAt:  email.ReceivedAt,
		},
	})
}

func (h *OTPShareHandler) respondOTPErr(c *gin.Context, status int, msg string) {
	if strings.EqualFold(strings.TrimSpace(c.Query("format")), "text") {
		c.Data(status, "text/plain; charset=utf-8", []byte(msg+"\n"))
		return
	}
	c.JSON(status, gin.H{"error": msg})
}

func buildOTPShareResponse(c *gin.Context, share *model.MailboxOTPShare) gin.H {
	url := buildPublicOTPShareURL(c, share.Token)
	apiURL := buildPublicOTPShareAPIURL(c)
	return gin.H{
		"mailbox_id":   share.MailboxID,
		"full_address": share.FullAddress,
		"token":        share.Token,
		"url":          url,
		"api_url":      apiURL,
		"curl":         fmt.Sprintf("curl -fsSL -H \"Authorization: Bearer %s\" '%s?format=text'", share.Token, apiURL),
		"created_at":   share.CreatedAt,
		"updated_at":   share.UpdatedAt,
	}
}

func buildPublicOTPShareURL(c *gin.Context, token string) string {
	return fmt.Sprintf("%s://%s/public/otp-share/%s/latest", detectRequestScheme(c), c.Request.Host, token)
}

func buildPublicOTPShareAPIURL(c *gin.Context) string {
	return fmt.Sprintf("%s://%s/public/otp-share/latest", detectRequestScheme(c), c.Request.Host)
}

func extractPublicOTPShareToken(c *gin.Context) string {
	if token := strings.TrimSpace(c.Param("token")); token != "" {
		return token
	}
	header := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	if header != "" {
		return header
	}
	return strings.TrimSpace(c.Query("token"))
}

func normalizeOTPShareToken(raw string) (string, error) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", nil
	}
	if !otpShareTokenPattern.MatchString(token) {
		return "", fmt.Errorf("invalid token: use 6-64 chars of letters, numbers, _ or -")
	}
	return token, nil
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
