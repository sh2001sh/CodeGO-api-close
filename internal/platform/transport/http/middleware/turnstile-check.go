package middleware

import (
	"encoding/json"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

var turnstileHTTPClient = &http.Client{Timeout: 5 * time.Second}

func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if platformconfig.TurnstileCheckEnabled {
			session := sessions.Default(c)
			turnstileChecked := session.Get("turnstile")
			if turnstileChecked != nil {
				c.Next()
				return
			}
			if !validateTurnstile(c) {
				return
			}
			session.Set("turnstile", true)
			err := session.Save()
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"message": "无法保存会话信息，请重试",
					"success": false,
				})
				return
			}
		}
		c.Next()
	}
}

// RegistrationTurnstileCheck requires a fresh Turnstile token for each
// account creation instead of accepting a previous session-level challenge.
func RegistrationTurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if platformconfig.TurnstileCheckEnabled && !validateTurnstile(c) {
			return
		}
		c.Next()
	}
}

func validateTurnstile(c *gin.Context) bool {
	response := c.Query("turnstile")
	if response == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Turnstile token 为空",
		})
		c.Abort()
		return false
	}

	req, err := http.NewRequestWithContext(
		c.Request.Context(),
		http.MethodPost,
		"https://challenges.cloudflare.com/turnstile/v0/siteverify",
		strings.NewReader(url.Values{
			"secret":   {platformconfig.TurnstileSecretKey},
			"response": {response},
			"remoteip": {c.ClientIP()},
		}.Encode()),
	)
	if err != nil {
		platformobservability.SysLog(err.Error())
		abortTurnstile(c, err.Error())
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rawRes, err := turnstileHTTPClient.Do(req)
	if err != nil {
		platformobservability.SysLog(err.Error())
		abortTurnstile(c, err.Error())
		return false
	}
	defer rawRes.Body.Close()

	var res turnstileCheckResponse
	if err := json.NewDecoder(rawRes.Body).Decode(&res); err != nil {
		platformobservability.SysLog(err.Error())
		abortTurnstile(c, err.Error())
		return false
	}
	if !res.Success {
		abortTurnstile(c, "Turnstile 校验失败，请刷新重试！")
		return false
	}
	return true
}

func abortTurnstile(c *gin.Context, message string) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": message,
	})
	c.Abort()
}
