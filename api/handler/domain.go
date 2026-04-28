package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"tempmail/cf"
	"tempmail/middleware"
	"tempmail/model"
	"tempmail/store"

	"github.com/gin-gonic/gin"
)

type DomainHandler struct {
	store       *store.Store
	cfgIP       string
	cfgHostname string
}

func NewDomainHandler(s *store.Store, smtpIP, smtpHostname string) *DomainHandler {
	return &DomainHandler{store: s, cfgIP: smtpIP, cfgHostname: smtpHostname}
}

func (h *DomainHandler) getServerIP(ctx context.Context) string {
	if ip, err := h.store.GetSetting(ctx, "smtp_server_ip"); err == nil && ip != "" {
		return ip
	}
	return h.cfgIP
}

func (h *DomainHandler) getServerHostname(ctx context.Context) string {
	if hn, err := h.store.GetSetting(ctx, "smtp_hostname"); err == nil && hn != "" {
		return hn
	}
	return h.cfgHostname
}

func (h *DomainHandler) getEffectiveHostname(ctx context.Context, domain, domainHostname string) string {
	if strings.TrimSpace(domainHostname) != "" {
		return strings.TrimSpace(domainHostname)
	}
	if global := h.getServerHostname(ctx); global != "" {
		return global
	}
	return "mail." + domain
}

func (h *DomainHandler) buildDNSRecords(ctx context.Context, domain, domainHostname string) []gin.H {
	serverIP := h.getServerIP(ctx)
	mxTarget := h.getEffectiveHostname(ctx, domain, domainHostname)
	usesCustomOrGlobalHostname := strings.TrimSpace(domainHostname) != "" || h.getServerHostname(ctx) != ""

	records := []gin.H{
		{"type": "MX", "host": "@", "value": mxTarget, "priority": 10, "description": "邮件交换记录，指向本服务器"},
		{"type": "TXT", "host": "@", "value": fmt.Sprintf("v=spf1 ip4:%s ~all", serverIP), "description": "SPF 记录（可选）"},
	}
	if !usesCustomOrGlobalHostname {
		records = []gin.H{
			{"type": "MX", "host": "@", "value": mxTarget, "priority": 10, "description": "邮件交换记录"},
			{"type": "A", "host": mxTarget, "value": serverIP, "description": "邮件服务器 A 记录"},
			{"type": "TXT", "host": "@", "value": fmt.Sprintf("v=spf1 ip4:%s ~all", serverIP), "description": "SPF 记录（可选）"},
		}
	}
	return records
}

func (h *DomainHandler) getCFClient(ctx context.Context) (*cf.Client, string, error) {
	token, err := h.store.GetSetting(ctx, "cf_api_token")
	if err != nil || strings.TrimSpace(token) == "" {
		return nil, "", fmt.Errorf("未配置 Cloudflare API Token，请在系统设置中添加 cf_api_token")
	}
	return cf.NewClient(strings.TrimSpace(token)), strings.TrimSpace(token), nil
}

// POST /api/admin/domains - 添加域名到池（管理员）
func (h *DomainHandler) Add(c *gin.Context) {
	var req struct {
		Domain   string `json:"domain" binding:"required"`
		Hostname string `json:"hostname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	req.Hostname = strings.TrimSpace(req.Hostname)

	domain, err := h.store.AddDomain(c.Request.Context(), req.Domain, req.Hostname)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "domain already exists: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"domain":       domain,
		"dns_records":  h.buildDNSRecords(c.Request.Context(), req.Domain, req.Hostname),
		"instructions": fmt.Sprintf("请在域名 %s 的 DNS 管理面板中添加以上记录。添加后约 5-30 分钟生效。", req.Domain),
	})
}

// GET /api/domains - 列出所有域名（共享域名池）
func (h *DomainHandler) List(c *gin.Context) {
	_ = middleware.GetAccount(c)

	filter := store.DomainFilter{
		Status:   strings.TrimSpace(c.Query("status")),
		Hostname: strings.TrimSpace(c.Query("hostname")),
		Query:    strings.TrimSpace(c.Query("q")),
	}

	domains, err := h.store.ListDomainsFiltered(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	summary, err := h.store.GetDomainSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"domains": domains, "summary": summary})
}

// DELETE /api/admin/domains/:id - 删除域名（管理员）
func (h *DomainHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain id"})
		return
	}

	if err := h.store.DeleteDomain(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "domain deleted"})
}

// PUT /api/admin/domains/:id/toggle - 启用/禁用域名（管理员）
func (h *DomainHandler) Toggle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain id"})
		return
	}

	var req struct {
		Active bool `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.store.ToggleDomain(c.Request.Context(), id, req.Active); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "domain updated"})
}

// PUT /api/admin/domains/:id/hostname - 更新域名 hostname
func (h *DomainHandler) UpdateHostname(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain id"})
		return
	}

	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.store.UpdateDomainHostname(c.Request.Context(), id, strings.TrimSpace(req.Hostname)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "hostname updated"})
}

// POST /api/admin/domains/mx-import - MX快捷接入（DNS检测并自动导入）
func (h *DomainHandler) MXImport(c *gin.Context) {
	var req struct {
		Domain   string `json:"domain" binding:"required"`
		Hostname string `json:"hostname"`
		Force    bool   `json:"force"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	req.Hostname = strings.TrimSpace(req.Hostname)

	serverIP := h.getServerIP(c.Request.Context())
	matched, mxHosts, mxStatus := store.CheckDomainMX(req.Domain, serverIP)
	if !matched && !req.Force {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":     "MX检测未通过，如确定要导入请加 force:true",
			"mx_status": mxStatus,
			"mx_hosts":  mxHosts,
			"server_ip": serverIP,
			"domain":    req.Domain,
			"dns_hint":  h.buildDNSRecords(c.Request.Context(), req.Domain, req.Hostname),
		})
		return
	}

	domain, err := h.store.AddDomain(c.Request.Context(), req.Domain, req.Hostname)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "域名已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"domain":     domain,
		"mx_status":  mxStatus,
		"mx_matched": matched,
		"message":    fmt.Sprintf("域名 %s 已导入域名池，Postfix 将在 60 秒内自动同步", req.Domain),
	})
}

// POST /api/admin/domains/mx-register - 提交域名等待自动MX验证（无需手动确认）
func (h *DomainHandler) MXRegister(c *gin.Context) {
	var req struct {
		Domain   string `json:"domain" binding:"required"`
		Hostname string `json:"hostname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	req.Hostname = strings.TrimSpace(req.Hostname)

	serverIP := h.getServerIP(c.Request.Context())
	matched, _, mxStatus := store.CheckDomainMX(req.Domain, serverIP)
	if matched {
		domain, err := h.store.AddDomain(c.Request.Context(), req.Domain, req.Hostname)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				existing, gerr := h.store.GetDomainByName(c.Request.Context(), req.Domain)
				if gerr == nil {
					c.JSON(http.StatusOK, gin.H{
						"domain":    existing,
						"status":    existing.Status,
						"mx_status": mxStatus,
						"message":   "域名已存在且处于激活状态",
					})
					return
				}
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{
			"domain":  domain,
			"status":  "active",
			"message": "MX验证通过，域名已立即加入域名池",
		})
		return
	}

	domain, err := h.store.AddDomainPending(c.Request.Context(), req.Domain, req.Hostname)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"domain":       domain,
		"status":       domain.Status,
		"server_ip":    serverIP,
		"mx_status":    mxStatus,
		"message":      fmt.Sprintf("域名 %s 已进入待验证队列，后台每30秒自动检测MX记录，通过后自动加入域名池", req.Domain),
		"dns_required": h.buildDNSRecords(c.Request.Context(), req.Domain, req.Hostname),
	})
}

// POST /api/domains/submit — 任意已登录用户提交域名进行 MX 自动验证
func (h *DomainHandler) Submit(c *gin.Context) {
	var req struct {
		Domain string `json:"domain" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Request.Body = http.NoBody

	serverIP := h.getServerIP(c.Request.Context())
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	matched, _, mxStatus := store.CheckDomainMX(domain, serverIP)
	if matched {
		d, err := h.store.AddDomain(c.Request.Context(), domain, "")
		if err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				existing, gerr := h.store.GetDomainByName(c.Request.Context(), domain)
				if gerr == nil {
					c.JSON(http.StatusOK, gin.H{"domain": existing, "status": existing.Status, "mx_status": mxStatus, "message": "域名已存在且处于激活状态"})
					return
				}
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"domain": d, "status": "active", "message": "MX验证通过，域名已立即加入域名池"})
		return
	}

	d, err := h.store.AddDomainPending(c.Request.Context(), domain, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"domain":       d,
		"status":       d.Status,
		"server_ip":    serverIP,
		"mx_status":    mxStatus,
		"message":      fmt.Sprintf("域名 %s 已进入待验证队列，后台每30秒自动检测MX记录，通过后自动加入域名池", domain),
		"dns_required": h.buildDNSRecords(c.Request.Context(), domain, ""),
	})
}

// GET /api/admin/domains/:id/status - 查询域名状态（用于前端轮询）
func (h *DomainHandler) GetStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain id"})
		return
	}

	domain, err := h.store.GetDomainByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":            domain.ID,
		"domain":        domain.Domain,
		"hostname":      domain.Hostname,
		"status":        domain.Status,
		"is_active":     domain.IsActive,
		"mx_checked_at": domain.MxCheckedAt,
	})
}

// POST /api/admin/domains/cf-create - 通过 Cloudflare API 自动创建子域名 MX 解析
func (h *DomainHandler) CFCreate(c *gin.Context) {
	var req struct {
		Domain   string `json:"domain" binding:"required"`
		Hostname string `json:"hostname"`
		Zone     string `json:"zone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	req.Hostname = strings.TrimSpace(req.Hostname)
	req.Zone = strings.TrimSpace(req.Zone)
	if req.Hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供 hostname（MX 记录目标，如 mail.example.com）"})
		return
	}

	client, _, err := h.getCFClient(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	zoneName := req.Zone
	if zoneName == "" {
		zoneName, err = cf.ExtractBaseDomain(req.Domain)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "域名格式不合法，至少需要子域名.主域名（如 sub.example.com）: " + err.Error(), "domain": req.Domain})
			return
		}
	}

	zone, err := client.FindZoneByName(zoneName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "查找 Cloudflare Zone 失败: " + err.Error(), "domain": req.Domain, "zone": zoneName})
		return
	}

	subdomain := strings.TrimSuffix(req.Domain, "."+zone.Name)
	if subdomain == req.Domain {
		subdomain = ""
	}

	existing, err := client.FindMXRecord(zone.ID, subdomain, zone.Name, req.Hostname)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "查询 Cloudflare DNS 记录失败: " + err.Error(), "zone": zone.Name, "subdomain": subdomain})
		return
	}

	skippedCF := existing != nil
	record := existing
	if !skippedCF {
		record, err = client.CreateMXRecord(zone.ID, subdomain, req.Hostname)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "创建 Cloudflare DNS 记录失败: " + err.Error(), "zone": zone.Name, "subdomain": subdomain, "mx_target": req.Hostname})
			return
		}
	}

	spfValue := fmt.Sprintf("v=spf1 ip4:%s ~all", h.getServerIP(c.Request.Context()))
	txtFQDN := zone.Name
	txtRecordName := zone.Name
	if subdomain != "" {
		txtFQDN = req.Domain
		txtRecordName = subdomain
	}
	txtRecord, err := client.FindDNSRecord(zone.ID, "TXT", txtFQDN, spfValue)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "查询 Cloudflare SPF TXT 记录失败: " + err.Error(), "zone": zone.Name, "txt_name": txtFQDN})
		return
	}
	if txtRecord == nil {
		if _, err := client.CreateTXTRecord(zone.ID, txtRecordName, spfValue); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "创建 Cloudflare SPF TXT 记录失败: " + err.Error(), "zone": zone.Name, "txt_name": txtFQDN, "txt_value": spfValue})
			return
		}
	}

	var domain *model.Domain
	if skippedCF {
		domain, err = h.store.AddDomain(c.Request.Context(), req.Domain, req.Hostname)
	} else {
		domain, err = h.store.AddDomainPending(c.Request.Context(), req.Domain, req.Hostname)
	}
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "域名已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	message := fmt.Sprintf("已在 Cloudflare Zone %s 中为 %s 创建 MX 记录（→ %s），域名已加入验证队列", zone.Name, req.Domain, req.Hostname)
	if skippedCF {
		message = fmt.Sprintf("Cloudflare Zone %s 中已存在 %s 的 MX 记录（→ %s），域名已直接激活", zone.Name, req.Domain, req.Hostname)
	}

	c.JSON(http.StatusCreated, gin.H{
		"domain":    domain,
		"cf_record": record,
		"zone":      zone.Name,
		"mx_target": req.Hostname,
		"message":   message,
	})
}

// DELETE /api/admin/domains/:id/cf - 删除 Cloudflare MX 后再删除本地域名
func (h *DomainHandler) CFDelete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain id"})
		return
	}

	domain, deletedCF, zoneName, err := h.deleteDomainWithCloudflare(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":           "域名已删除",
		"domain":            domain.Domain,
		"zone":              zoneName,
		"cf_record_deleted": deletedCF,
	})
}

// PUT /api/admin/domains/batch/toggle - 批量启用/禁用域名
func (h *DomainHandler) BatchToggle(c *gin.Context) {
	var req struct {
		IDs    []int `json:"ids" binding:"required"`
		Active bool  `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updated, err := h.store.BatchToggleDomains(c.Request.Context(), req.IDs, req.Active)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": updated, "total": len(req.IDs)})
}

// PUT /api/admin/domains/batch/delete - 批量删除域名
func (h *DomainHandler) BatchDelete(c *gin.Context) {
	var req struct {
		IDs              []int `json:"ids" binding:"required"`
		DeleteCloudflare bool  `json:"delete_cloudflare"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	deleted := 0
	errorsList := []gin.H{}
	for _, id := range req.IDs {
		if req.DeleteCloudflare {
			domain, deletedCF, zoneName, err := h.deleteDomainWithCloudflare(c.Request.Context(), id)
			if err != nil {
				errorsList = append(errorsList, gin.H{"id": id, "error": err.Error()})
				continue
			}
			deleted++
			errorsList = append(errorsList, gin.H{"id": id, "domain": domain.Domain, "zone": zoneName, "cf_record_deleted": deletedCF, "status": "deleted"})
			continue
		}
		if err := h.store.DeleteDomain(c.Request.Context(), id); err != nil {
			errorsList = append(errorsList, gin.H{"id": id, "error": err.Error()})
			continue
		}
		deleted++
	}

	c.JSON(http.StatusOK, gin.H{"deleted": deleted, "total": len(req.IDs), "results": errorsList})
}

func (h *DomainHandler) deleteDomainWithCloudflare(ctx context.Context, id int) (*model.Domain, bool, string, error) {
	domain, err := h.store.GetDomainByID(ctx, id)
	if err != nil {
		return nil, false, "", fmt.Errorf("domain not found")
	}
	client, _, err := h.getCFClient(ctx)
	if err != nil {
		return nil, false, "", err
	}
	targetHostname := h.getEffectiveHostname(ctx, domain.Domain, domain.Hostname)

	zoneName, err := cf.ExtractBaseDomain(domain.Domain)
	if err != nil {
		return nil, false, "", fmt.Errorf("域名格式不合法: %w", err)
	}
	zone, err := client.FindZoneByName(zoneName)
	if err != nil {
		return nil, false, zoneName, fmt.Errorf("查找 Cloudflare Zone 失败: %w", err)
	}

	subdomain := strings.TrimSuffix(domain.Domain, "."+zone.Name)
	if subdomain == domain.Domain {
		subdomain = ""
	}
	record, err := client.FindMXRecord(zone.ID, subdomain, zone.Name, targetHostname)
	if err != nil {
		return nil, false, zone.Name, fmt.Errorf("查找 MX 记录失败: %w", err)
	}

	spfValue := fmt.Sprintf("v=spf1 ip4:%s ~all", h.getServerIP(ctx))
	txtFQDN := zone.Name
	if subdomain != "" {
		txtFQDN = domain.Domain
	}
	txtRecord, err := client.FindDNSRecord(zone.ID, "TXT", txtFQDN, "")
	if err != nil {
		return nil, false, zone.Name, fmt.Errorf("查找 TXT 记录失败: %w", err)
	}

	deletedCF := false
	if record != nil {
		if err := client.DeleteDNSRecord(zone.ID, record.ID); err != nil {
			return nil, false, zone.Name, fmt.Errorf("删除 Cloudflare MX 记录失败: %w", err)
		}
		deletedCF = true
	}
	if txtRecord != nil {
		content := strings.Trim(txtRecord.Content, "\"")
		if content == spfValue {
			if err := client.DeleteDNSRecord(zone.ID, txtRecord.ID); err != nil {
				return nil, deletedCF, zone.Name, fmt.Errorf("删除 Cloudflare TXT 记录失败: %w", err)
			}
			deletedCF = true
		}
	}

	if err := h.store.DeleteDomain(ctx, id); err != nil {
		return nil, deletedCF, zone.Name, fmt.Errorf("删除本地域名失败: %w", err)
	}
	return domain, deletedCF, zone.Name, nil
}
