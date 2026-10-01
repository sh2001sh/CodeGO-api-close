package legacy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSecurityPersistentFieldsKeepNullsAndExactIDs(t *testing.T) {
	raw := json.RawMessage(`{"id":"old-string-id","dedupe_key":"old-dedupe","source":"upstream_policy","decision":"blocked","risk_code":"policy","severity":"high","notification_targets":2,"notification_success":1,"review_status":"unreviewed","user_id":9007199254740993,"reviewed_at":null,"updated_at":null,"created_at":"2025-01-01T01:02:03.123456Z"}`)
	a, err := decodeSecurityAudit(raw)
	if err != nil || a.UserID == nil || *a.UserID != 9007199254740993 || a.UpdatedAt != nil || a.ReviewedAt != nil || a.CreatedAt == nil || a.CreatedAt.Nanosecond() != 123456000 {
		t.Fatalf("audit exact bigint/null/time fields were lost: err=%v", err)
	}
	s, err := decodeSecurityState(json.RawMessage(`{"user_id":9007199254740993,"strikes":2,"restricted_until":1800000100,"last_window_end":1800000000,"blocked":true,"evidence":null,"updated_at":null}`))
	if err != nil || s.UserID != 9007199254740993 || !s.Blocked || s.Evidence != nil || s.UpdatedAt != nil {
		t.Fatalf("state exact bigint/null fields were lost: err=%v", err)
	}
}

func TestSecurityInvalidPersistenceRejectedWithoutEvidenceLeak(t *testing.T) {
	for _, raw := range []string{
		`{"user_id":7,"strikes":-1,"restricted_until":0,"last_window_end":0,"blocked":false}`,
		`{"user_id":7,"strikes":2147483648,"restricted_until":0,"last_window_end":0,"blocked":false}`,
		`{"user_id":7,"strikes":0,"restricted_until":-1,"last_window_end":0,"blocked":false}`,
		`{"user_id":7,"strikes":0,"restricted_until":0,"last_window_end":0,"blocked":null}`,
	} {
		if _, err := decodeSecurityState(json.RawMessage(raw)); err == nil {
			t.Fatal("unsafe restriction accepted")
		}
	}
	fields := map[string]any{"id": "audit", "dedupe_key": "dedupe", "source": "guard", "decision": "blocked", "risk_code": "risk", "severity": "high", "notification_targets": 0, "notification_success": 0, "review_status": "unreviewed", "upstream_error_body": "dummy-private-evidence"}
	for _, bad := range []struct {
		name  string
		value any
	}{
		{"id", ""}, {"review_status", nil}, {"prompt_preview", strings.Repeat("界", 513)},
		{"created_at", "invalid-time"}, {"http_status", int64(2147483648)},
	} {
		copy := make(map[string]any, len(fields)+1)
		for name, value := range fields {
			copy[name] = value
		}
		copy[bad.name] = bad.value
		raw, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		_, err = decodeSecurityAudit(raw)
		if err == nil || strings.Contains(err.Error(), "dummy-private-evidence") {
			t.Fatalf("invalid audit must reject with safe error for %s", bad.name)
		}
	}
	fields["prompt_preview"] = strings.Repeat("界", 512)
	raw, _ := json.Marshal(fields)
	if _, err := decodeSecurityAudit(raw); err != nil {
		t.Fatalf("varchar character boundary rejected: %v", err)
	}
}
