package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
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
	smtpHostname, _ := h.store.GetDefaultHostname(c.Request.Context())
	hostnames, _ := h.store.ListHostnames(c.Request.Context(), true)
	announce, _ := h.store.GetSetting(c.Request.Context(), "announcement")
	mailboxPageSize, _ := h.store.GetSetting(c.Request.Context(), "mailbox_page_size")
	c.JSON(http.StatusOK, gin.H{
		"registration_open": regOpen == "true",
		"site_title":        siteTitle,
		"smtp_server_ip":    smtpIP,
		"smtp_hostname":     smtpHostname,
		"hostnames":         hostnames,
		"announcement":      announce,
		"mailbox_page_size": mailboxPageSize,
	})
}

// GET /api/admin/settings → 读取所有设置（管理员）
func (h *SettingHandler) AdminGetAll(c *gin.Context) {
	settings, err := h.store.GetAllSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if smtpHostname, err := h.store.GetDefaultHostname(c.Request.Context()); err == nil {
		settings["smtp_hostname"] = smtpHostname
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
		"mailbox_page_size":       true,
		"otp_segmented_enabled":   true,
		"otp_segmented_lengths":   true,
		"otp_segmented_senders":   true,
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

	normalizedReq := make(map[string]string, len(req))
	for k, v := range req {
		if !allowed[k] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown setting key: " + k})
			return
		}
		if k == "mailbox_page_size" {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 1 || n > 24 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "mailbox_page_size must be an integer between 1 and 24"})
				return
			}
			v = strconv.Itoa(n)
		}
		if k == "otp_segmented_enabled" {
			v = strings.ToLower(strings.TrimSpace(v))
			if v != "true" && v != "false" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "otp_segmented_enabled must be true or false"})
				return
			}
		}
		if k == "otp_segmented_lengths" {
			seen := map[string]bool{}
			var normalized []string
			for _, item := range strings.FieldsFunc(v, func(r rune) bool {
				return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\r' || r == '\t'
			}) {
				item = strings.TrimSpace(item)
				if item != "3" && item != "4" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "otp_segmented_lengths only supports 3 and 4"})
					return
				}
				if !seen[item] {
					seen[item] = true
					normalized = append(normalized, item)
				}
			}
			if len(normalized) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "otp_segmented_lengths must select 3 or 4"})
				return
			}
			v = strings.Join(normalized, ",")
		}
		if k == "otp_segmented_senders" {
			seen := map[string]bool{}
			var normalized []string
			for _, item := range strings.FieldsFunc(v, func(r rune) bool {
				return r == ',' || r == ';' || r == '\n' || r == '\r'
			}) {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				parsed, err := mail.ParseAddress(item)
				if err != nil || strings.TrimSpace(parsed.Address) == "" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "invalid otp segmented sender: " + item})
					return
				}
				address := strings.ToLower(strings.TrimSpace(parsed.Address))
				if !seen[address] {
					seen[address] = true
					normalized = append(normalized, address)
				}
			}
			v = strings.Join(normalized, "\n")
		}
		if k == "smtp_hostname" {
			v = strings.ToLower(strings.TrimSpace(v))
		}
		normalizedReq[k] = v
	}

	for k, v := range normalizedReq {
		if k == "smtp_hostname" {
			if v != "" {
				if _, err := h.store.UpsertHostname(c.Request.Context(), v); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
			}
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
