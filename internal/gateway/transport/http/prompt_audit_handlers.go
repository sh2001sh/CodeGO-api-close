package http

import (
	stdhttp "net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	securityaudit "github.com/sh2001sh/new-api/internal/gateway/securityaudit"
)

const maxPromptAuditRecordLimit = 512

// GetPromptAuditMetrics exposes content-free Guard telemetry to root operators.
func GetPromptAuditMetrics(c *gin.Context) {
	service := securityaudit.DefaultService()
	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"mode":    service.Mode(),
			"metrics": service.Metrics(),
			"records": service.AuditRecords(promptAuditRecordLimit(c.Query("limit"))),
		},
	})
}

func promptAuditRecordLimit(raw string) int {
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 50
	}
	if limit > maxPromptAuditRecordLimit {
		return maxPromptAuditRecordLimit
	}
	return limit
}
