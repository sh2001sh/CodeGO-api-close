package desktop

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

var sensitive = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]+`),
	regexp.MustCompile(`(?i)\b(?:sk-|desktop_)[A-Za-z0-9_-]{12,}\b`),
	regexp.MustCompile(`(?im)(?:api[_ -]?key|access[_ -]?token|authorization)\s*[:=]\s*["']?[^"'\s,;]+`),
	regexp.MustCompile(`[A-Za-z]:\\(?:[^\\\r\n\t ]+\\)*[^\\\r\n\t ]*`),
	regexp.MustCompile(`/(?:Users|home)/[^/\s]+(?:/[^\s]*)?`),
}
var sensitiveKey = regexp.MustCompile(`(?i)api.?key|access.?token|authorization|password|secret`)

func scrub(raw string) string {
	for _, p := range sensitive {
		raw = p.ReplaceAllString(raw, "[REDACTED]")
	}
	return raw
}

type ReportInput struct {
	ReportType string          `json:"report_type,omitempty"`
	EventName  string          `json:"event_name,omitempty"`
	Source     string          `json:"source"`
	Summary    string          `json:"summary,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	AppVersion string          `json:"app_version"`
	Platform   string          `json:"platform"`
	Locale     string          `json:"locale"`
	Consent    bool            `json:"consent"`
}

func cleanReport(in ReportInput, telemetry bool) (ReportInput, error) {
	if !in.Consent || len(in.Source) > 64 || len(in.Summary) > 500 || len(in.AppVersion) > 64 || len(in.Platform) > 64 || len(in.Locale) > 32 {
		return in, ErrInvalid
	}
	in.Source = scrub(in.Source)
	in.Summary = scrub(in.Summary)
	in.AppVersion = scrub(in.AppVersion)
	in.Platform = scrub(in.Platform)
	in.Locale = scrub(in.Locale)
	if telemetry {
		switch in.EventName {
		case "auth_connected", "summary_refreshed", "diagnostic_report_submitted":
		default:
			return in, ErrInvalid
		}
		var payload map[string]any
		if len(in.Payload) == 0 {
			in.Payload = json.RawMessage(`{}`)
		}
		if json.Unmarshal(in.Payload, &payload) != nil || payload == nil || len(payload) > 12 {
			return in, ErrInvalid
		}
		clean := map[string]any{}
		for key, value := range payload {
			if len(key) > 64 {
				return in, ErrInvalid
			}
			if sensitiveKey.MatchString(key) {
				clean[scrub(key)] = "[REDACTED]"
				continue
			}
			switch v := value.(type) {
			case string:
				clean[scrub(key)] = scrub(v)
			case bool, float64, nil:
				clean[scrub(key)] = v
			default:
				return in, ErrInvalid
			}
		}
		b, err := json.Marshal(clean)
		if err != nil || len(b) > 4000 {
			return in, ErrInvalid
		}
		in.Payload = b
	} else {
		switch strings.ToLower(in.ReportType) {
		case "panic", "crash":
			in.ReportType = "crash"
		case "manual", "support":
			in.ReportType = "manual"
		default:
			return in, ErrInvalid
		}
		var payload string
		if json.Unmarshal(in.Payload, &payload) != nil || len(payload) == 0 || len(payload) > 12000 {
			return in, ErrInvalid
		}
		b, err := json.Marshal(scrub(payload))
		if err != nil {
			return in, err
		}
		in.Payload = b
	}
	return in, nil
}
func (s *Service) reportHTTP(w http.ResponseWriter, r *http.Request) {
	telemetry := strings.Contains(r.URL.Path, "/telemetry/")
	scope := "config:write"
	kind := "diagnostic"
	if telemetry {
		scope = "telemetry:write"
		kind = "telemetry"
	}
	d, ok := s.device(w, r, scope)
	if !ok {
		return
	}
	var in ReportInput
	if err := decode(w, r, &in); err != nil {
		reply(w, nil, err)
		return
	}
	in, err := cleanReport(in, telemetry)
	if err != nil {
		reply(w, nil, err)
		return
	}
	b, err := json.Marshal(in)
	if err != nil {
		reply(w, nil, err)
		return
	}
	var id int64
	err = s.pool.QueryRow(r.Context(), `INSERT INTO v3_identity.desktop_reports(user_id,device_id,kind,payload) VALUES($1,$2,$3,$4) RETURNING id`, d.UserID, d.ID, kind, b).Scan(&id)
	if telemetry {
		reply(w, true, err)
	} else {
		reply(w, map[string]any{"id": id, "status": "received"}, err)
	}
}
