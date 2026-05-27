package api

import (
	"fmt"
	"net"
	"net/http"
	"net/url"

	"vless-audit/internal/model"

	"github.com/gin-gonic/gin"
)

// RegisterHandler handles POST /api/register — self-service user registration.
func (h *Handler) RegisterHandler(c *gin.Context) {
	var req struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Secret      string `json:"secret"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}

	// Check registration secret.
	if h.RegisterSecret == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "暂未开放注册"})
		return
	}
	if req.Secret != h.RegisterSecret {
		c.JSON(http.StatusForbidden, gin.H{"error": "口令错误，无法注册"})
		return
	}

	// Check if user already exists.
	existing, _ := h.Store.GetUser(req.Email)
	if existing != nil {
		// Return existing config.
		c.JSON(http.StatusOK, gin.H{
			"uuid":      existing.UUID,
			"email":     existing.Email,
			"vless_url": buildVLESSURL(c, existing.UUID, existing.Email),
			"exists":    true,
		})
		return
	}

	// Create new user.
	user := &model.VLESSUser{
		Email:       req.Email,
		DisplayName: req.DisplayName,
		UUID:        generateUUID(),
		Level:       0,
		Enable:      true,
	}
	if err := h.Store.CreateUser(user); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "创建失败: " + err.Error()})
		return
	}

	// Sync to Xray config.
	if h.XrayConfigPath != "" {
		_ = SyncXrayUsers(h.Store, h.XrayConfigPath, h.XrayBinPath)
	}

	c.JSON(http.StatusOK, gin.H{
		"uuid":         user.UUID,
		"email":        user.Email,
		"display_name": user.DisplayName,
		"vless_url":    buildVLESSURL(c, user.UUID, user.Email),
		"exists":       false,
	})
}

// RegisterLookup handles GET /api/register/:email — lookup existing user config.
func (h *Handler) RegisterLookup(c *gin.Context) {
	email := c.Param("email")
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email required"})
		return
	}
	user, err := h.Store.GetUser(email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"uuid":      user.UUID,
		"email":     user.Email,
		"vless_url": buildVLESSURL(c, user.UUID, user.Email),
	})
}

func buildVLESSURL(c *gin.Context, uuid, email string) string {
	host := c.Request.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	// Default to standard VLESS port if not specified.
	return fmt.Sprintf("vless://%s@%s:443?encryption=none&type=tcp&security=none#%s",
		uuid, host, url.QueryEscape(email))
}
