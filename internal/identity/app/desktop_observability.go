package app

import (
	"encoding/json"
	"fmt"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"regexp"
	"slices"
	"strings"
)

const (
	maxDesktopDiagnosticSummaryLength = 500
	maxDesktopDiagnosticPayloadLength = 12000
	maxDesktopTelemetryPayloadLength  = 4000
	maxDesktopTelemetryProperties     = 12
)

var (
	allowedDesktopTelemetryEvents = []string{
		"auth_connected",
		"summary_refreshed",
		"diagnostic_report_submitted",
	}
	desktopSensitiveBearerPattern      = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]+`)
	desktopSensitiveAuthHeaderPattern  = regexp.MustCompile(`(?im)(authorization\s*[:=]\s*)([^\s"']+)`)
	desktopSensitiveAPIKeyPattern      = regexp.MustCompile(`(?im)((?:api[_ -]?key|access[_ -]?token)\s*[:=]\s*["']?)([^"'\s,;]+)`)
	desktopSensitiveOpenAIKeyPattern   = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`)
	desktopSensitiveWindowsPathPattern = regexp.MustCompile(`[A-Za-z]:\\(?:[^\\\r\n\t ]+\\)*[^\\\r\n\t ]*`)
	desktopSensitiveMacHomePattern     = regexp.MustCompile(`/Users/[^/\s]+(?:/[^\s]*)?`)
	desktopSensitiveLinuxHomePattern   = regexp.MustCompile(`/home/[^/\s]+(?:/[^\s]*)?`)
)

type DesktopDiagnosticReportRequest struct {
	ReportType string `json:"report_type"`
	Source     string `json:"source"`
	Summary    string `json:"summary"`
	Payload    string `json:"payload"`
	AppVersion string `json:"app_version"`
	Platform   string `json:"platform"`
	Locale     string `json:"locale"`
	Consent    bool   `json:"consent"`
}

type DesktopTelemetryEventRequest struct {
	EventName  string         `json:"event_name"`
	Source     string         `json:"source"`
	Payload    map[string]any `json:"payload"`
	AppVersion string         `json:"app_version"`
	Platform   string         `json:"platform"`
	Locale     string         `json:"locale"`
	Consent    bool           `json:"consent"`
}

// CreateDesktopDiagnosticReport validates, sanitizes, and stores a desktop diagnostic report.
func CreateDesktopDiagnosticReport(userID int, deviceID int, deviceName string, req DesktopDiagnosticReportRequest) (*identitydomain.DesktopDiagnosticReport, error) {
	reportType := normalizeDesktopDiagnosticType(req.ReportType)
	if reportType == "" {
		return nil, fmt.Errorf("invalid diagnostic report type")
	}
	if !req.Consent {
		return nil, fmt.Errorf("diagnostic report requires explicit consent")
	}

	summary := sanitizeDesktopSensitiveText(req.Summary, maxDesktopDiagnosticSummaryLength)
	payload := sanitizeDesktopSensitiveText(req.Payload, maxDesktopDiagnosticPayloadLength)
	if payload == "" {
		return nil, fmt.Errorf("diagnostic report payload is required")
	}

	report := &identitydomain.DesktopDiagnosticReport{
		UserID:     userID,
		DeviceID:   deviceID,
		DeviceName: deviceName,
		ReportType: reportType,
		Source:     normalizeDesktopMetadataSource(req.Source),
		Summary:    summary,
		Payload:    payload,
		AppVersion: limitDesktopMetadataValue(req.AppVersion, 64),
		Platform:   limitDesktopMetadataValue(req.Platform, 64),
		Locale:     limitDesktopMetadataValue(req.Locale, 32),
		Consent:    true,
		CreatedAt:  platformruntime.GetTimestamp(),
	}

	if err := platformdb.DB.Create(report).Error; err != nil {
		return nil, err
	}

	auditapp.RecordLog(report.UserID, auditschema.LogTypeManage, fmt.Sprintf("submitted desktop diagnostic report %d", report.Id))
	return report, nil
}

// CreateDesktopTelemetryEvent validates, sanitizes, and stores a desktop telemetry event.
func CreateDesktopTelemetryEvent(userID int, deviceID int, deviceName string, req DesktopTelemetryEventRequest) (*identitydomain.DesktopTelemetryEvent, error) {
	eventName := normalizeDesktopTelemetryEventName(req.EventName)
	if eventName == "" {
		return nil, fmt.Errorf("invalid telemetry event name")
	}
	if !req.Consent {
		return nil, fmt.Errorf("telemetry event requires explicit consent")
	}

	payload, err := sanitizeDesktopTelemetryPayload(req.Payload)
	if err != nil {
		return nil, err
	}

	event := &identitydomain.DesktopTelemetryEvent{
		UserID:     userID,
		DeviceID:   deviceID,
		DeviceName: deviceName,
		EventName:  eventName,
		Source:     normalizeDesktopMetadataSource(req.Source),
		Payload:    payload,
		AppVersion: limitDesktopMetadataValue(req.AppVersion, 64),
		Platform:   limitDesktopMetadataValue(req.Platform, 64),
		Locale:     limitDesktopMetadataValue(req.Locale, 32),
		Consent:    true,
		CreatedAt:  platformruntime.GetTimestamp(),
	}

	if err := platformdb.DB.Create(event).Error; err != nil {
		return nil, err
	}

	return event, nil
}

func sanitizeDesktopSensitiveText(raw string, limit int) string {
	value := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if value == "" {
		return ""
	}

	value = desktopSensitiveBearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = desktopSensitiveAuthHeaderPattern.ReplaceAllString(value, "${1}[REDACTED]")
	value = desktopSensitiveAPIKeyPattern.ReplaceAllString(value, "${1}[REDACTED]")
	value = desktopSensitiveOpenAIKeyPattern.ReplaceAllString(value, "[REDACTED_API_KEY]")
	value = desktopSensitiveWindowsPathPattern.ReplaceAllString(value, "[REDACTED_PATH]")
	value = desktopSensitiveMacHomePattern.ReplaceAllString(value, "[REDACTED_PATH]")
	value = desktopSensitiveLinuxHomePattern.ReplaceAllString(value, "[REDACTED_PATH]")

	if limit > 0 && len(value) > limit {
		value = strings.TrimSpace(value[:limit]) + "\n[TRUNCATED]"
	}
	return value
}

func normalizeDesktopDiagnosticType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "panic", "crash":
		return "crash"
	case "manual", "support":
		return "manual"
	default:
		return ""
	}
}

func normalizeDesktopTelemetryEventName(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if !slices.Contains(allowedDesktopTelemetryEvents, value) {
		return ""
	}
	return value
}

func normalizeDesktopMetadataSource(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "desktop"
	}
	if len(value) > 64 {
		value = value[:64]
	}
	return value
}

func limitDesktopMetadataValue(raw string, maxLength int) string {
	value := strings.TrimSpace(raw)
	if maxLength > 0 && len(value) > maxLength {
		return value[:maxLength]
	}
	return value
}

func sanitizeDesktopTelemetryPayload(raw map[string]any) (string, error) {
	if len(raw) == 0 {
		return "{}", nil
	}

	payload := make(map[string]any, min(len(raw), maxDesktopTelemetryProperties))
	count := 0
	for key, value := range raw {
		if count >= maxDesktopTelemetryProperties {
			break
		}
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			continue
		}
		if len(normalizedKey) > 64 {
			normalizedKey = normalizedKey[:64]
		}

		switch typed := value.(type) {
		case string:
			payload[normalizedKey] = sanitizeDesktopSensitiveText(typed, 0)
		case bool:
			payload[normalizedKey] = typed
		case float64, float32, int, int32, int64, uint, uint32, uint64:
			payload[normalizedKey] = typed
		case nil:
			payload[normalizedKey] = nil
		default:
			payload[normalizedKey] = sanitizeDesktopSensitiveText(fmt.Sprint(typed), 0)
		}
		count++
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	value := string(encoded)
	if len(value) > maxDesktopTelemetryPayloadLength {
		value = value[:maxDesktopTelemetryPayloadLength]
	}
	return value, nil
}
