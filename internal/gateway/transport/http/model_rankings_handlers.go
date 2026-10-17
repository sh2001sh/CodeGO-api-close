package http

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

func GetModelRankingSummary(c *gin.Context) {
	result, err := gatewayroutingapp.BuildModelRankingSummary(parsePerfMetricsHours(c.Query("hours")))
	if err != nil {
		platformobservability.SysError("model ranking summary failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "model rankings unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func GetModelRankingTrend(c *gin.Context) {
	model := strings.TrimSpace(c.Query("model"))
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "model is required"})
		return
	}
	result, err := gatewayroutingapp.BuildModelRankingTrend(model, parsePerfMetricsHours(c.Query("hours")))
	if err != nil {
		platformobservability.SysError("model ranking trend failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "model rankings unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
