package http

import (
	"github.com/gin-gonic/gin"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

// CreateDesktopDiagnosticReport stores a desktop diagnostic report submitted by an authorized desktop client.
func CreateDesktopDiagnosticReport(c *gin.Context) {
	var req identityapp.DesktopDiagnosticReportRequest
	if err := platformhttpx.UnmarshalBodyReusable(c, &req); err != nil {
		httpapi.ApiError(c, err)
		return
	}

	report, err := identityapp.CreateDesktopDiagnosticReport(
		c.GetInt("id"),
		c.GetInt("desktop_device_id"),
		c.GetString("desktop_device_name"),
		req,
	)
	if err != nil {
		httpapi.ApiErrorMsg(c, err.Error())
		return
	}

	httpapi.ApiSuccess(c, gin.H{
		"id":     report.Id,
		"status": "received",
	})
}

// CreateDesktopTelemetryEvent stores a desktop telemetry event submitted by an authorized desktop client.
func CreateDesktopTelemetryEvent(c *gin.Context) {
	var req identityapp.DesktopTelemetryEventRequest
	if err := platformhttpx.UnmarshalBodyReusable(c, &req); err != nil {
		httpapi.ApiError(c, err)
		return
	}

	if _, err := identityapp.CreateDesktopTelemetryEvent(
		c.GetInt("id"),
		c.GetInt("desktop_device_id"),
		c.GetString("desktop_device_name"),
		req,
	); err != nil {
		httpapi.ApiErrorMsg(c, err.Error())
		return
	}

	httpapi.ApiSuccess(c, true)
}
