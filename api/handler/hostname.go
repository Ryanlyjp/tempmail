package handler

import (
	"net/http"
	"strconv"
	"strings"

	"tempmail/store"

	"github.com/gin-gonic/gin"
)

type HostnameHandler struct {
	store *store.Store
}

func NewHostnameHandler(s *store.Store) *HostnameHandler {
	return &HostnameHandler{store: s}
}

// GET /api/hostnames - 返回已启用 hostname，供下拉选择
func (h *HostnameHandler) List(c *gin.Context) {
	hostnames, err := h.store.ListHostnames(c.Request.Context(), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"hostnames": hostnames})
}

// GET /api/admin/hostnames - 管理员查看全部 hostname
func (h *HostnameHandler) AdminList(c *gin.Context) {
	hostnames, err := h.store.ListHostnames(c.Request.Context(), false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"hostnames": hostnames})
}

// POST /api/admin/hostnames - 新增 hostname
func (h *HostnameHandler) Add(c *gin.Context) {
	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Hostname = strings.TrimSpace(req.Hostname)
	if req.Hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hostname 不能为空"})
		return
	}

	hostname, err := h.store.CreateHostname(c.Request.Context(), req.Hostname)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "hostname 已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"hostname": hostname})
}

// PUT /api/admin/hostnames/:id - 编辑 hostname
func (h *HostnameHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hostname id"})
		return
	}

	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Hostname = strings.TrimSpace(req.Hostname)
	if req.Hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hostname 不能为空"})
		return
	}

	hostname, err := h.store.UpdateHostname(c.Request.Context(), id, req.Hostname)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "hostname 已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "hostname updated", "hostname": hostname})
}

// PUT /api/admin/hostnames/:id/toggle - 启用/禁用 hostname
func (h *HostnameHandler) Toggle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hostname id"})
		return
	}

	var req struct {
		Active bool `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.store.ToggleHostname(c.Request.Context(), id, req.Active); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "hostname updated"})
}

// DELETE /api/admin/hostnames/:id - 删除 hostname，并清理已绑定域名的 hostname 引用
func (h *HostnameHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hostname id"})
		return
	}

	cleared, err := h.store.DeleteHostname(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "hostname deleted", "cleared_domains": cleared})
}
