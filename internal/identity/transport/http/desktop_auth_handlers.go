package http

import (
	"errors"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	"gorm.io/gorm"
)

func StartDesktopAuthSession(c *gin.Context) {
	var req identityapp.DesktopAuthStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	payload, err := identityapp.StartDesktopAuthSession(req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetDesktopAuthSession(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	userCode := strings.TrimSpace(c.Query("code"))
	if sessionID == "" || userCode == "" {
		httpapi.ApiErrorMsg(c, "missing session_id or code")
		return
	}
	payload, err := identityapp.BuildDesktopAuthSessionView(sessionID, userCode)
	if err != nil {
		if err.Error() == "desktop authorization code does not match" || err.Error() == "desktop authorization session has expired" {
			httpapi.ApiErrorMsg(c, err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpapi.ApiErrorMsg(c, "desktop authorization session was not found")
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func ApproveDesktopAuthSession(c *gin.Context) {
	var req identityapp.DesktopAuthorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	payload, err := identityapp.ApproveDesktopAuthSession(c.GetInt("id"), req.SessionID)
	if err != nil {
		if err.Error() == "desktop authorization session has expired" || err.Error() == "desktop authorization session has already been handled" {
			httpapi.ApiErrorMsg(c, err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpapi.ApiErrorMsg(c, "desktop authorization session was not found")
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func RejectDesktopAuthSession(c *gin.Context) {
	var req identityapp.DesktopAuthorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	payload, err := identityapp.RejectDesktopAuthSession(c.GetInt("id"), req.SessionID)
	if err != nil {
		if err.Error() == "desktop authorization session has expired" || err.Error() == "desktop authorization session has already been handled" {
			httpapi.ApiErrorMsg(c, err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpapi.ApiErrorMsg(c, "desktop authorization session was not found")
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func PollDesktopAuthSession(c *gin.Context) {
	var req identityapp.DesktopAuthPollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	payload, err := identityapp.PollDesktopAuthSession(req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpapi.ApiErrorMsg(c, "desktop authorization session was not found")
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func ListDesktopAuthorizedDevices(c *gin.Context) {
	items, err := identityapp.ListDesktopAuthorizedDevices(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, items)
}

func RevokeDesktopAuthorizedDevice(c *gin.Context) {
	deviceID, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil {
		httpapi.ApiErrorMsg(c, "invalid device id")
		return
	}
	if err := identityapp.RevokeDesktopAuthorizedDevice(c.GetInt("id"), deviceID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpapi.ApiErrorMsg(c, "desktop device was not found")
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, true)
}
