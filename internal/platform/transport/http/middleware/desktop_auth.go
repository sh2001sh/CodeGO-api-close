package middleware

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/i18n"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	"gorm.io/gorm"
	"net/http"
	"strings"
)

func DesktopAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authorization := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
			authorization = strings.TrimSpace(authorization[7:])
		}
		if authorization == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": httpapi.TranslateMessage(c, i18n.MsgAuthNotLoggedIn),
			})
			c.Abort()
			return
		}

		device, err := identityapp.ValidateDesktopDeviceAccessToken(authorization)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"message": "desktop access token is invalid",
				})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"message": err.Error(),
				})
			}
			c.Abort()
			return
		}

		apiUserIDStr := strings.TrimSpace(c.GetHeader("New-Api-User"))
		if apiUserIDStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": httpapi.TranslateMessage(c, i18n.MsgAuthUserIdNotProvided),
			})
			c.Abort()
			return
		}
		userIdentifierMatches, err := matchesAuthenticatedNewAPIUser(apiUserIDStr, device.UserID)
		if err != nil && !errors.Is(err, errInvalidNewAPIUserIdentifier) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": httpapi.TranslateMessage(c, i18n.MsgDatabaseError),
			})
			c.Abort()
			return
		}
		if errors.Is(err, errInvalidNewAPIUserIdentifier) || !userIdentifierMatches {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": httpapi.TranslateMessage(c, i18n.MsgAuthUserIdMismatch),
			})
			c.Abort()
			return
		}

		user, err := identityapp.LoadUserByID(device.UserID, false)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": httpapi.TranslateMessage(c, i18n.MsgDatabaseError),
			})
			c.Abort()
			return
		}
		if user.Status != constant.UserStatusEnabled {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": httpapi.TranslateMessage(c, i18n.MsgAuthUserBanned),
			})
			c.Abort()
			return
		}

		_ = identityapp.TouchDesktopDevice(device.Id)

		c.Header("Auth-Version", "desktop-v1")
		c.Set("username", user.Username)
		c.Set("role", user.Role)
		c.Set("id", user.Id)
		c.Set("group", user.Group)
		c.Set("user_group", user.Group)
		c.Set("desktop_device_id", device.Id)
		c.Set("desktop_device_name", device.DeviceName)
		c.Set("desktop_device_scopes", identityapp.ParseDesktopScopes(device.Scopes))
		c.Set("desktop_device", device)
		c.Set("use_access_token", true)
		if !enforceGlobalAuthenticatedAPIRateLimit(c) {
			return
		}
		c.Next()
	}
}

func RequireDesktopScope(required string) gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceValue, exists := c.Get("desktop_device")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "desktop device context is missing",
			})
			c.Abort()
			return
		}

		device, ok := deviceValue.(*identitydomain.DesktopAuthorizedDevice)
		if !ok || device == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "desktop device context is invalid",
			})
			c.Abort()
			return
		}

		if !identityapp.DesktopDeviceHasScope(device, required) {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "desktop device is missing required scope: " + required,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
