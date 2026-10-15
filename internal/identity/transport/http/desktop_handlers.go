package http

import (
	"errors"
	"github.com/gin-gonic/gin"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	"gorm.io/gorm"
	"strconv"
	"strings"
)

func GetDesktopAccountSummary(c *gin.Context) {
	payload, err := identityapp.BuildDesktopAccountSummary(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetDesktopUsageLogs(c *gin.Context) {
	pageInfo := platformpagination.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	page, err := identityapp.BuildDesktopUsageLogsPage(
		c.GetInt("id"),
		pageInfo,
		logType,
		startTimestamp,
		endTimestamp,
		c.Query("model_name"),
		c.Query("token_name"),
		c.Query("group"),
		c.Query("request_id"),
		c.Query("upstream_request_id"),
	)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, page)
}

func GetDesktopUsageTrends(c *gin.Context) {
	days, _ := strconv.Atoi(c.Query("days"))
	if days <= 0 {
		days = 7
	}
	if days > 30 {
		days = 30
	}

	payload, err := identityapp.BuildDesktopUsageTrend(c.GetInt("id"), days)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetDesktopGroups(c *gin.Context) {
	payload, err := identityapp.GetDesktopGroupsForUser(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func EnsureDesktopToken(c *gin.Context) {
	var req identityapp.DesktopEnsureTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	payload, err := identityapp.EnsureDesktopToken(c.GetInt("id"), req)
	if err != nil {
		httpapi.ApiErrorMsg(c, err.Error())
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetDesktopTokens(c *gin.Context) {
	page, err := identityapp.BuildDesktopTokensPage(c.GetInt("id"), platformpagination.GetPageQuery(c))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, page)
}

func GetDesktopTokenKey(c *gin.Context) {
	tokenID, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil {
		httpapi.ApiErrorMsg(c, "invalid token id")
		return
	}
	token, err := identityapp.FindDesktopTokenByID(c.GetInt("id"), tokenID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpapi.ApiErrorMsg(c, "token not found")
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"key": token.GetFullKey()})
}

func UpdateDesktopTokenGroup(c *gin.Context) {
	tokenID, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil {
		httpapi.ApiErrorMsg(c, "invalid token id")
		return
	}

	var req identityapp.DesktopUpdateTokenGroupRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}

	token, err := identityapp.UpdateDesktopTokenGroup(c.GetInt("id"), tokenID, req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpapi.ApiErrorMsg(c, "token not found")
			return
		}
		httpapi.ApiErrorMsg(c, err.Error())
		return
	}
	httpapi.ApiSuccess(c, token)
}

func GetDesktopConfigTemplate(c *gin.Context) {
	template, err := identityapp.BuildDesktopConfigTemplate(c.Query("tool"))
	if err != nil {
		httpapi.ApiErrorMsg(c, err.Error())
		return
	}
	httpapi.ApiSuccess(c, template)
}

func GetDesktopConfigTemplates(c *gin.Context) {
	payload, err := identityapp.BuildDesktopConfigTemplatesResponse()
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetDesktopTokenConfig(c *gin.Context) {
	tokenID, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil {
		httpapi.ApiErrorMsg(c, "invalid token id")
		return
	}

	payload, err := identityapp.BuildDesktopTokenConfigForUser(c.GetInt("id"), tokenID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpapi.ApiErrorMsg(c, "token not found")
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetDesktopServiceStatus(c *gin.Context) {
	httpapi.ApiSuccess(c, identityapp.DesktopServiceStatusSummary())
}

func CreateDesktopImportConfig(c *gin.Context) {
	var req identityapp.DesktopImportCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	payload, err := identityapp.BuildDesktopImportConfigDeepLink(c.GetInt("id"), req)
	if err != nil {
		if err.Error() == "unsupported desktop target" || err.Error() == "unsupported tool" || err.Error() == "invalid token_id" || err.Error() == "token is not enabled" {
			httpapi.ApiErrorMsg(c, err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpapi.ApiErrorMsg(c, "token not found")
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetDesktopImportConfig(c *gin.Context) {
	payload, err := identityapp.ResolveDesktopImportConfig(c.Query("code"))
	if err != nil {
		httpapi.ApiErrorMsg(c, err.Error())
		return
	}
	httpapi.ApiSuccess(c, payload)
}
