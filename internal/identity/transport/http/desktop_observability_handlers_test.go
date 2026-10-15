package http

import (
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/sh2001sh/new-api/constant"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	"net/http"
	"strings"
	"testing"
)

func TestCreateDesktopDiagnosticReportStoresSanitizedPayload(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)

	user := &identityschema.User{
		Id:          1,
		Username:    "desktop-diagnostic-user",
		Password:    "password123",
		DisplayName: "Desktop Diagnostic User",
		Role:        constant.RoleCommonUser,
		Status:      constant.UserStatusEnabled,
		Group:       "default",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	body := map[string]any{
		"report_type": "crash",
		"source":      "panic_hook",
		"summary":     `panic while calling Authorization: Bearer sk-secret-token-1234567890`,
		"payload": "Message: panic at C:\\Users\\alice\\workspace\\app.rs\n" +
			`Authorization: Bearer desktop-secret-token-123456` + "\n" +
			`api_key="sk-another-secret-123456"` + "\n" +
			`Working Dir: /Users/alice/project`,
		"app_version": "3.16.4",
		"platform":    "windows",
		"locale":      "en-US",
		"consent":     true,
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/diagnostics/report", body, 1)
	ctx.Set("desktop_device_id", 77)
	ctx.Set("desktop_device_name", "QA Desktop")
	CreateDesktopDiagnosticReport(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var report identitydomain.DesktopDiagnosticReport
	if err := db.First(&report).Error; err != nil {
		t.Fatalf("failed to load diagnostic report: %v", err)
	}

	if report.DeviceID != 77 || report.DeviceName != "QA Desktop" {
		t.Fatalf("unexpected device metadata: %+v", report)
	}
	if strings.Contains(report.Payload, "desktop-secret-token") || strings.Contains(report.Payload, "sk-another-secret") {
		t.Fatalf("payload should be redacted, got %s", report.Payload)
	}
	if strings.Contains(report.Payload, `C:\Users\alice`) || strings.Contains(report.Payload, `/Users/alice/`) {
		t.Fatalf("payload should redact local paths, got %s", report.Payload)
	}
	if !strings.Contains(report.Payload, "[REDACTED]") || !strings.Contains(report.Payload, "[REDACTED_PATH]") {
		t.Fatalf("payload redaction markers missing: %s", report.Payload)
	}
}

func TestCreateDesktopDiagnosticReportRequiresConsent(t *testing.T) {
	setupDesktopHTTPTestDB(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/diagnostics/report", map[string]any{
		"report_type": "crash",
		"payload":     "panic",
		"consent":     false,
	}, 1)
	ctx.Set("desktop_device_id", 7)
	ctx.Set("desktop_device_name", "QA Desktop")
	CreateDesktopDiagnosticReport(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 api envelope, got %d", recorder.Code)
	}

	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected api failure when consent missing")
	}
	if !strings.Contains(response.Message, "explicit consent") {
		t.Fatalf("unexpected message: %s", response.Message)
	}
}

func TestCreateDesktopTelemetryEventStoresSanitizedPayload(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)

	user := &identityschema.User{
		Id:          1,
		Username:    "desktop-telemetry-user",
		Password:    "password123",
		DisplayName: "Desktop Telemetry User",
		Role:        constant.RoleCommonUser,
		Status:      constant.UserStatusEnabled,
		Group:       "default",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	body := map[string]any{
		"event_name":  "summary_refreshed",
		"source":      "dashboard",
		"app_version": "3.16.4",
		"platform":    "windows",
		"locale":      "en-US",
		"consent":     true,
		"payload": map[string]any{
			"trigger":       "manual",
			"serviceStatus": "ok",
			"path":          `C:\Users\alice\workspace`,
			"token":         "Bearer sk-secret-telemetry-token-123456",
			"lowBalance":    true,
		},
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/telemetry/events", body, 1)
	ctx.Set("desktop_device_id", 88)
	ctx.Set("desktop_device_name", "QA Desktop")
	CreateDesktopTelemetryEvent(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var event identitydomain.DesktopTelemetryEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("failed to load telemetry event: %v", err)
	}

	if event.DeviceID != 88 || event.EventName != "summary_refreshed" {
		t.Fatalf("unexpected telemetry event: %+v", event)
	}
	if strings.Contains(event.Payload, "alice") || strings.Contains(event.Payload, "sk-secret") {
		t.Fatalf("payload should be sanitized, got %s", event.Payload)
	}
	if !strings.Contains(event.Payload, "[REDACTED_PATH]") || !strings.Contains(event.Payload, "[REDACTED]") {
		t.Fatalf("expected redaction markers in payload: %s", event.Payload)
	}
}

func TestCreateDesktopTelemetryEventRejectsUnknownEvent(t *testing.T) {
	setupDesktopHTTPTestDB(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/telemetry/events", map[string]any{
		"event_name": "unknown_event",
		"payload":    map[string]any{"trigger": "manual"},
		"consent":    true,
	}, 1)
	ctx.Set("desktop_device_id", 88)
	ctx.Set("desktop_device_name", "QA Desktop")
	CreateDesktopTelemetryEvent(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 api envelope, got %d", recorder.Code)
	}

	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected api failure for unknown telemetry event")
	}
	if !strings.Contains(response.Message, "invalid telemetry event") {
		t.Fatalf("unexpected message: %s", response.Message)
	}
}
