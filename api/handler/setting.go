package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"tempmail/middleware"
	"tempmail/store"
	"tempmail/telegrambot"

	"github.com/gin-gonic/gin"
)

type SettingHandler struct {
	store *store.Store
}

func NewSettingHandler(s *store.Store) *SettingHandler {
	return &SettingHandler{store: s}
}

// GET /public/settings → 返回前端需要的公开配置
func (h *SettingHandler) GetPublic(c *gin.Context) {
	regOpen, err := h.store.GetSetting(c.Request.Context(), "registration_open")
	if err != nil {
		regOpen = "false"
	}
	siteTitle, _ := h.store.GetSetting(c.Request.Context(), "site_title")
	smtpIP, _ := h.store.GetSetting(c.Request.Context(), "smtp_server_ip")
	smtpHostname, _ := h.store.GetSetting(c.Request.Context(), "smtp_hostname")
	announce, _ := h.store.GetSetting(c.Request.Context(), "announcement")
	c.JSON(http.StatusOK, gin.H{
		"registration_open": regOpen == "true",
		"site_title":        siteTitle,
		"smtp_server_ip":    smtpIP,
		"smtp_hostname":     smtpHostname,
		"announcement":      announce,
	})
}

// GET /api/admin/settings → 读取所有设置（管理员）
func (h *SettingHandler) AdminGetAll(c *gin.Context) {
	settings, err := h.store.GetAllSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, settings)
}

// PUT /api/admin/settings → 更新设置（管理员）
func (h *SettingHandler) AdminUpdate(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 白名单：已知配置项
	allowed := map[string]bool{
		"registration_open":       true,
		"rate_limit_enabled":      true,
		"max_mailboxes_per_user":  true,
		"smtp_server_ip":          true,
		"smtp_hostname":           true,
		"site_title":              true,
		"announcement":            true,
		"default_domain":          true,
		"mailbox_ttl_minutes":     true,
		"api_mailbox_ttl_minutes": true,
		"catchall_enabled":        true,
		"catchall_account_id":     true,
		"cf_api_token":            true,
		"api_subdomain_enabled":   true,
		"api_subdomain_length":    true,
		"api_domain_strategy":     true,
		"api_domain_fixed":        true,
		"tg_bot_token":            true,
		"tg_chat_id":              true,
		"tg_message_thread_id":    true,
		"tg_forward_mode":         true,
	}

	for k, v := range req {
		if !allowed[k] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown setting key: " + k})
			return
		}
		if err := h.store.SetSetting(c.Request.Context(), k, v); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "settings updated"})
}

// POST /api/admin/settings/tg/test → 发送 TG 测试消息（管理员）
func (h *SettingHandler) AdminTestTelegram(c *gin.Context) {
	account := middleware.GetAccount(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
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

	text := fmt.Sprintf(
		"TempMail TG 连通性测试\n时间: %s\n账号: %s\n模式: %s\n状态: 配置已生效",
		time.Now().Format("2006-01-02 15:04:05"),
		account.Username,
		telegrambot.NormalizeMode(cfg.Mode),
	)
	if err := telegrambot.SendTestMessage(ctx, cfg, text); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "telegram test sent"})
}
