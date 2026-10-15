package middleware

import (
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"net/http"
	"strings"

	identityapp "github.com/sh2001sh/new-api/internal/identity/app"

	"github.com/gin-gonic/gin"
)

func abortMiniProgramAuth(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{
		"success": false,
		"message": message,
	})
	c.Abort()
}

// MiniProgramAuth validates the mini program bearer token and loads binding context.
func MiniProgramAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		rawToken := strings.TrimSpace(c.Request.Header.Get("Authorization"))
		claims, err := identityapp.ParseMiniProgramSessionToken(rawToken)
		if err != nil {
			abortMiniProgramAuth(c, http.StatusUnauthorized, err.Error())
			return
		}

		c.Set("mini_openid", claims.OpenID)
		c.Set("mini_unionid", claims.UnionID)
		c.Set("mini_token_expires_at", claims.ExpiresAt)

		if err := identityapp.TouchMiniProgramBindingByOpenID(claims.OpenID); err != nil {
			platformobservability.SysLog("failed to update mini program binding last_seen_at: " + err.Error())
		}

		authContext, err := identityapp.LoadMiniProgramAuthContext(claims.OpenID)
		if err != nil {
			abortMiniProgramAuth(c, http.StatusInternalServerError, "failed to load mini program binding")
			return
		}
		if authContext != nil {
			c.Set("mini_bound_user_id", authContext.BoundUserID)
			c.Set("mini_binding_status", authContext.BindingStatus)
			c.Set("id", authContext.BoundUserID)
			c.Set("username", authContext.Username)
			c.Set("group", authContext.Group)
			c.Set("user_group", authContext.Group)
		}

		c.Next()
	}
}

// MiniProgramBoundAuth requires the mini program session to be bound to a website account.
func MiniProgramBoundAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		if c.GetInt("id") <= 0 {
			abortMiniProgramAuth(c, http.StatusForbidden, "mini program account is not bound")
			return
		}
		c.Next()
	}
}
