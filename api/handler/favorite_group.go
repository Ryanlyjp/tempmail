package handler

import (
	"errors"
	"net/http"
	"strings"

	"tempmail/middleware"
	"tempmail/store"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type FavoriteGroupHandler struct {
	store *store.Store
}

func NewFavoriteGroupHandler(s *store.Store) *FavoriteGroupHandler {
	return &FavoriteGroupHandler{store: s}
}

func validateFavoriteGroupName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, name != "" && len([]rune(name)) <= 32
}

func (h *FavoriteGroupHandler) List(c *gin.Context) {
	account := middleware.GetAccount(c)
	groups, selectedID, err := h.store.ListFavoriteGroups(c.Request.Context(), account.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups, "selected_group_id": selectedID})
}

func (h *FavoriteGroupHandler) Create(c *gin.Context) {
	account := middleware.GetAccount(c)
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, ok := validateFavoriteGroupName(req.Name)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group name must be 1-32 characters"})
		return
	}
	group, err := h.store.CreateFavoriteGroup(c.Request.Context(), account.ID, name)
	if errors.Is(err, store.ErrFavoriteGroupConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "favorite group name already exists"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"group": group})
}

func (h *FavoriteGroupHandler) Rename(c *gin.Context) {
	account := middleware.GetAccount(c)
	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid favorite group id"})
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, ok := validateFavoriteGroupName(req.Name)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group name must be 1-32 characters"})
		return
	}
	group, err := h.store.RenameFavoriteGroup(c.Request.Context(), account.ID, groupID, name)
	if errors.Is(err, store.ErrFavoriteGroupConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "favorite group name already exists"})
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "favorite group not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"group": group})
}

func (h *FavoriteGroupHandler) Select(c *gin.Context) {
	account := middleware.GetAccount(c)
	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid favorite group id"})
		return
	}
	if err := h.store.SelectFavoriteGroup(c.Request.Context(), account.ID, groupID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "favorite group not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"selected_group_id": groupID})
}

func (h *FavoriteGroupHandler) Reorder(c *gin.Context) {
	account := middleware.GetAccount(c)
	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid favorite group id"})
		return
	}
	var req struct {
		Direction string `json:"direction"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Direction = strings.ToLower(strings.TrimSpace(req.Direction))
	if req.Direction != "up" && req.Direction != "down" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "direction must be up or down"})
		return
	}
	if err := h.store.ReorderFavoriteGroup(c.Request.Context(), account.ID, groupID, req.Direction); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "favorite group not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "favorite group reordered"})
}

func (h *FavoriteGroupHandler) Delete(c *gin.Context) {
	account := middleware.GetAccount(c)
	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid favorite group id"})
		return
	}
	err = h.store.DeleteFavoriteGroup(c.Request.Context(), account.ID, groupID)
	if errors.Is(err, store.ErrLastFavoriteGroup) {
		c.JSON(http.StatusConflict, gin.H{"error": "at least one favorite group is required"})
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "favorite group not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "favorite group deleted"})
}

func (h *FavoriteGroupHandler) MoveMailbox(c *gin.Context) {
	account := middleware.GetAccount(c)
	mailboxID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}
	var req struct {
		GroupID uuid.UUID `json:"group_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.GroupID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid group_id is required"})
		return
	}
	mailbox, err := h.store.MoveMailboxToFavoriteGroup(c.Request.Context(), mailboxID, account.ID, req.GroupID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox or favorite group not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"mailbox": mailbox})
}

func parseMailboxIDs(c *gin.Context) ([]uuid.UUID, bool) {
	var req struct {
		IDs     []uuid.UUID `json:"ids"`
		GroupID uuid.UUID   `json:"group_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 || len(req.IDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ids must contain 1-100 mailbox ids"})
		return nil, false
	}
	return req.IDs, true
}

func (h *FavoriteGroupHandler) BatchMove(c *gin.Context) {
	account := middleware.GetAccount(c)
	var req struct {
		IDs     []uuid.UUID `json:"ids"`
		GroupID uuid.UUID   `json:"group_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 || len(req.IDs) > 100 || req.GroupID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ids and valid group_id are required"})
		return
	}
	affected, err := h.store.MoveMailboxesToFavoriteGroup(c.Request.Context(), req.IDs, account.ID, req.GroupID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailboxes or favorite group not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"affected": affected})
}

func (h *FavoriteGroupHandler) BatchDelete(c *gin.Context) {
	account := middleware.GetAccount(c)
	ids, ok := parseMailboxIDs(c)
	if !ok {
		return
	}
	affected, err := h.store.DeleteMailboxes(c.Request.Context(), ids, account.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"affected": affected})
}
