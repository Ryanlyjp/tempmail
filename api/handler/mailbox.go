package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"tempmail/middleware"
	"tempmail/model"
	"tempmail/store"

	"github.com/gin-gonic/gin"
)

type MailboxHandler struct {
	store *store.Store
}

func NewMailboxHandler(s *store.Store) *MailboxHandler {
	return &MailboxHandler{store: s}
}

// POST /api/mailboxes - 创建临时邮箱
// 请求体字段均为可选：
//
//	address          — 本地部分（@ 前），为空则随机生成
//	domain           — 指定域名（须是已激活域名），为空则按全局策略选取
//	subdomain_mode   — "off" | "random" | "custom"；为空时按全局默认（仅当目标域名启用了多级子域时才生效）
//	subdomain        — 仅 mode=custom 必填；规则 ^[a-z0-9]{2,8}$
//	subdomain_length — mode=random 时可选，2-8；优先级：请求 > 域名设置 > 全局默认 > 5
//
// 回退规则：mode != off 但所选域名 subdomain_enabled=false → 静默回退 v1，
// 响应里加 fallback_reason="domain has subdomain disabled"。
func (h *MailboxHandler) Create(c *gin.Context) {
	account := middleware.GetAccount(c)

	var req struct {
		Address         string `json:"address"`
		Domain          string `json:"domain"`
		SubdomainMode   string `json:"subdomain_mode"`
		Subdomain       string `json:"subdomain"`
		SubdomainLength int    `json:"subdomain_length"`
	}
	c.ShouldBindJSON(&req)

	ctx := c.Request.Context()

	address := strings.TrimSpace(req.Address)
	if address == "" {
		address = store.GenerateRandomAddress()
	}
	address = strings.ToLower(address)

	// 读取 TTL 设置
	ttlMinutes := 30
	if ttlStr, err := h.store.GetSetting(ctx, "mailbox_ttl_minutes"); err == nil {
		if n, err := strconv.Atoi(ttlStr); err == nil && n > 0 {
			ttlMinutes = n
		}
	}

	// 全局 API 默认配置（仅当请求未显式指定时使用）
	apiSubEnabled, _ := h.store.GetSetting(ctx, "api_subdomain_enabled")
	apiSubLengthStr, _ := h.store.GetSetting(ctx, "api_subdomain_length")
	apiDomainStrategy, _ := h.store.GetSetting(ctx, "api_domain_strategy")
	apiDomainFixed, _ := h.store.GetSetting(ctx, "api_domain_fixed")
	defaultSubLength := store.SubdomainDefault
	if n, err := strconv.Atoi(strings.TrimSpace(apiSubLengthStr)); err == nil {
		defaultSubLength = store.ClampSubdomainLength(n)
	}

	// 确定域名：请求显式 > 全局策略 fixed > 全局策略 random
	var domainRecord *model.Domain
	if d := strings.TrimSpace(strings.ToLower(req.Domain)); d != "" {
		found, err := h.store.GetDomainByName(ctx, d)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "domain not found or not active: " + d})
			return
		}
		domainRecord = found
	} else if strings.TrimSpace(apiDomainStrategy) == "fixed" && strings.TrimSpace(apiDomainFixed) != "" {
		found, err := h.store.GetDomainByName(ctx, strings.TrimSpace(apiDomainFixed))
		if err != nil {
			// 配置的固定域不可用 → 退回随机
			fallbackRandom, ferr := h.store.GetRandomActiveDomain(ctx)
			if ferr != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no active domains available"})
				return
			}
			domainRecord = fallbackRandom
		} else {
			domainRecord = found
		}
	} else {
		found, err := h.store.GetRandomActiveDomain(ctx)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no active domains available"})
			return
		}
		domainRecord = found
	}

	// 解析子域模式
	mode := strings.ToLower(strings.TrimSpace(req.SubdomainMode))
	if mode == "" {
		// 请求未带 → 走全局默认
		if apiSubEnabled == "true" {
			mode = "random"
		} else {
			mode = "off"
		}
	}
	if mode != "off" && mode != "random" && mode != "custom" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid subdomain_mode: " + mode})
		return
	}

	// 静默回退：v2 请求遇到未启用域名 → 退到 v1
	fallbackReason := ""
	if mode != "off" && !domainRecord.SubdomainEnabled {
		mode = "off"
		fallbackReason = "domain has subdomain disabled"
	}

	// 生成最终 fullAddress
	formatVersion := "v1"
	fullAddress := fmt.Sprintf("%s@%s", address, domainRecord.Domain)
	switch mode {
	case "custom":
		sub, err := store.ValidateSubdomain(req.Subdomain)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		fullAddress = fmt.Sprintf("%s@%s.%s", address, sub, domainRecord.Domain)
		formatVersion = "v2"
	case "random":
		// 长度优先级：请求 > 域名设置 > 全局默认
		length := req.SubdomainLength
		if length < store.SubdomainMin || length > store.SubdomainMax {
			if domainRecord.SubdomainRandomLength >= store.SubdomainMin && domainRecord.SubdomainRandomLength <= store.SubdomainMax {
				length = domainRecord.SubdomainRandomLength
			} else {
				length = defaultSubLength
			}
		}
		sub := store.GenerateRandomSubdomain(length)
		fullAddress = fmt.Sprintf("%s@%s.%s", address, sub, domainRecord.Domain)
		formatVersion = "v2"
	}

	mailbox, err := h.store.CreateMailbox(ctx, account.ID, address, domainRecord.ID, fullAddress, ttlMinutes)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "address already taken, try again"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := gin.H{
		"mailbox":        mailbox,
		"format_version": formatVersion,
	}
	if fallbackReason != "" {
		resp["fallback_reason"] = fallbackReason
	}
	c.JSON(http.StatusCreated, resp)
}

// GET /api/mailboxes - 列出当前账号的邮箱
func (h *MailboxHandler) List(c *gin.Context) {
	account := middleware.GetAccount(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 { page = 1 }
	if size < 1 || size > 100 { size = 20 }

	mailboxes, total, err := h.store.ListMailboxes(c.Request.Context(), account.ID, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  mailboxes,
		"total": total,
		"page":  page,
		"size":  size,
	})
}

// PUT /api/mailboxes/:id/favorite - 设置/取消收藏
// 收藏后邮箱不会被定时清理；取消收藏会把过期时间重置为 now + mailbox_ttl_minutes。
func (h *MailboxHandler) Favorite(c *gin.Context) {
	account := middleware.GetAccount(c)
	id, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	var req struct {
		Favorite bool `json:"favorite"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ttlMinutes := 30
	if ttlStr, err := h.store.GetSetting(c.Request.Context(), "mailbox_ttl_minutes"); err == nil {
		if n, err := strconv.Atoi(ttlStr); err == nil && n > 0 {
			ttlMinutes = n
		}
	}

	mailbox, err := h.store.SetMailboxFavorite(c.Request.Context(), id, account.ID, req.Favorite, ttlMinutes)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"mailbox": mailbox})
}

// DELETE /api/mailboxes/:id - 删除邮箱
func (h *MailboxHandler) Delete(c *gin.Context) {
	account := middleware.GetAccount(c)
	id, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	if err := h.store.DeleteMailbox(c.Request.Context(), id, account.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "mailbox deleted"})
}
