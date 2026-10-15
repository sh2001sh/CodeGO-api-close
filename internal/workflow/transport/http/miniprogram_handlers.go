package http

import (
	"github.com/gin-gonic/gin"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	identityhttp "github.com/sh2001sh/new-api/internal/identity/transport/http"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	workflowapp "github.com/sh2001sh/new-api/internal/workflow/app"
	"strconv"
)

func MiniProgramSession(c *gin.Context) {
	identityhttp.MiniProgramSession(c)
}

func GetMiniProgramMe(c *gin.Context) {
	identityhttp.GetMiniProgramMe(c)
}

func BindMiniProgram(c *gin.Context) {
	identityhttp.BindMiniProgram(c)
}

func UnbindMiniProgram(c *gin.Context) {
	identityhttp.UnbindMiniProgram(c)
}

func CheckMiniProgramShareContent(c *gin.Context) {
	identityhttp.CheckMiniProgramShareContent(c)
}

func GetMiniProgramDashboard(c *gin.Context) {
	days := workflowapp.NormalizeMiniProgramWindowDays(c.Query("days"), 7)
	payload, err := workflowapp.BuildMiniProgramDashboard(c.GetInt("id"), days)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetMiniProgramLogs(c *gin.Context) {
	pageInfo := platformpagination.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	logs, total, err := auditapp.ListUserLogs(c.GetInt("id"), auditapp.LogListQuery{
		LogType:           logType,
		StartTimestamp:    startTimestamp,
		EndTimestamp:      endTimestamp,
		ModelName:         c.Query("model_name"),
		TokenName:         c.Query("token_name"),
		Group:             c.Query("group"),
		RequestID:         c.Query("request_id"),
		UpstreamRequestID: c.Query("upstream_request_id"),
		StartIdx:          pageInfo.GetStartIdx(),
		PageSize:          pageInfo.GetPageSize(),
	})
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	httpapi.ApiSuccess(c, pageInfo)
}

func GetMiniProgramStat(c *gin.Context) {
	days := workflowapp.NormalizeMiniProgramWindowDays(c.Query("days"), 7)
	payload, err := workflowapp.BuildMiniProgramStat(c.GetInt("id"), days)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetMiniProgramGeneMap(c *gin.Context) {
	days := workflowapp.NormalizeMiniProgramWindowDays(c.Query("days"), 30)
	payload, err := workflowapp.BuildMiniProgramGeneMap(c.GetInt("id"), days)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}
