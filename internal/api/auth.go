package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware checks the X-Auth-Token header against the configured token.
func AuthMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Allow login endpoint without auth.
		if c.Request.URL.Path == "/api/auth/login" || strings.HasPrefix(c.Request.URL.Path, "/api/register") {
			c.Next()
			return
		}

		// Allow static assets.
		if strings.HasPrefix(c.Request.URL.Path, "/app/") || c.Request.URL.Path == "/" {
			c.Next()
			return
		}

		auth := c.GetHeader("X-Auth-Token")
		if auth == "" {
			auth = c.Query("token")
		}
		if auth == "" {
			// Check cookie.
			if cookie, err := c.Cookie("auth_token"); err == nil {
				auth = cookie
			}
		}
		if auth != token || token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// LoginHandler handles POST /api/auth/login.
func LoginHandler(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		if req.Password != token {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token, "message": "登录成功"})
	}
}
