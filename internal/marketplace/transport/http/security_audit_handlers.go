package http

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	securityaudit "github.com/sh2001sh/new-api/internal/gateway/securityaudit"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func ListOwnerSecurityAuditEvents(c *gin.Context) {
	listSecurityAuditEvents(c, false)
}

func ListAdminSecurityAuditEvents(c *gin.Context) {
	listSecurityAuditEvents(c, true)
}

func listSecurityAuditEvents(c *gin.Context, admin bool) {
	result, err := securityaudit.ListEvents(securityAuditEventQuery(c, admin))
	respond(c, result, err)
}

func securityAuditEventQuery(c *gin.Context, admin bool) securityaudit.EventQuery {
	return securityaudit.EventQuery{
		ViewerUserID: c.GetInt("id"), Admin: admin,
		Page: queryInt(c, "page", 1), PageSize: queryInt(c, "page_size", 20),
		Source: c.Query("source"), ReviewStatus: c.Query("review_status"),
		MarketplaceChannel: c.Query("channel_id"), Model: c.Query("model"), Search: c.Query("search"),
		StartTimestamp: queryInt64(c, "start_timestamp"), EndTimestamp: queryInt64(c, "end_timestamp"),
	}
}

func ExportOwnerSecurityAuditEvents(c *gin.Context) {
	items, err := securityaudit.ExportEvents(securityAuditEventQuery(c, false))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	payload, err := securityAuditCSV(items)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	filename := "security-audit-" + time.Now().Format("20060102-150405") + ".csv"
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", payload)
}

func securityAuditCSV(items []gatewayschema.SecurityAuditEvent) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(&buffer)
	if err := writer.Write([]string{
		"事件 ID", "请求 ID", "触发时间", "来源", "风险等级", "风险代码", "处理状态",
		"渠道 ID", "市场分组", "用户 ID", "Key 名称", "模型", "协议", "HTTP 状态",
		"上游错误类型", "上游错误代码", "上游错误", "Prompt 脱敏摘要", "Prompt SHA-256",
		"计费结果", "通知状态", "通知成功数", "通知目标数", "近 24 小时触发次数",
		"复核备注", "复核人", "复核时间",
	}); err != nil {
		return nil, err
	}
	for i := range items {
		item := items[i]
		reviewedAt := ""
		if item.ReviewedAt != nil {
			reviewedAt = item.ReviewedAt.Local().Format("2006-01-02 15:04:05")
		}
		row := []string{
			item.ID, item.RequestID, item.CreatedAt.Local().Format("2006-01-02 15:04:05"), item.Source,
			item.Severity, item.RiskCode, item.ReviewStatus, item.MarketplaceChannelID,
			item.MarketplaceGroupID, strconv.Itoa(item.UserID), item.TokenName, item.Model, item.Protocol,
			strconv.Itoa(item.HTTPStatus), item.UpstreamErrorType, item.UpstreamErrorCode,
			item.UpstreamErrorMessage, item.PromptPreview, item.PromptHash, item.BillingResult,
			item.NotificationStatus, strconv.Itoa(item.NotificationSuccess), strconv.Itoa(item.NotificationTargets),
			strconv.FormatInt(item.RecentTriggerCount, 10), item.ReviewNote, strconv.Itoa(item.ReviewedBy), reviewedAt,
		}
		for index := range row {
			row[index] = safeCSVCell(row[index])
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("生成安全审计 CSV 失败: %w", err)
	}
	return buffer.Bytes(), nil
}

func safeCSVCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}

func UpdateOwnerSecurityAuditEvent(c *gin.Context) {
	updateSecurityAuditEvent(c, false)
}

func UpdateAdminSecurityAuditEvent(c *gin.Context) {
	updateSecurityAuditEvent(c, true)
}

func updateSecurityAuditEvent(c *gin.Context, admin bool) {
	var request struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := securityaudit.UpdateEventReview(c.GetInt("id"), admin, c.Param("id"), request.Status, request.Note)
	respond(c, result, err)
}
