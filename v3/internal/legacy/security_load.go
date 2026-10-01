package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type securityData struct {
	source  pgx.Tx
	sources map[string]string
	counts  map[string]int64
	issues  []Issue
}

func loadSecurityData(ctx context.Context, source pgx.Tx, sources map[string]string) (*securityData, error) {
	d := &securityData{source: source, sources: sources, counts: map[string]int64{}}
	users := map[int64]bool{}
	if err := walkHistory(ctx, source, sources["users"], func(raw json.RawMessage) error {
		var u struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			return fmt.Errorf("legacy: invalid security user reference")
		}
		users[u.ID] = true
		return nil
	}); err != nil {
		return nil, err
	}
	for _, name := range []string{"security_audit_events", "account_request_abuse_states"} {
		err := walkHistory(ctx, source, sources[name], func(raw json.RawMessage) error {
			d.counts[name]++
			if name == "security_audit_events" {
				a, err := decodeSecurityAudit(raw)
				if err != nil {
					d.issues = append(d.issues, Issue{name, 0, "invalid_security_audit", err.Error()})
					return nil
				}
				if a.UserID != nil && *a.UserID > 0 && !users[*a.UserID] {
					d.counts["historical_deleted_user_references"]++
				}
				return nil
			}
			s, err := decodeSecurityState(raw)
			if err != nil {
				d.issues = append(d.issues, Issue{name, 0, "invalid_security_state", err.Error()})
				return nil
			}
			if !users[s.UserID] {
				d.issues = append(d.issues, Issue{name, s.UserID, "missing_user", "operational restriction references a missing user"})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (d *securityData) validate(report *Report) {
	report.Issues = append(report.Issues, d.issues...)
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	for name, count := range d.counts {
		report.Counts["security."+name] = count
	}
}

func decodeSecurityState(raw json.RawMessage) (sourceSecurityState, error) {
	var s sourceSecurityState
	if err := securityRequiredFields(raw, "user_id", "strikes", "restricted_until", "last_window_end", "blocked"); err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("invalid persistent restriction fields")
	}
	if s.UserID <= 0 || s.Strikes < 0 || s.RestrictedUntil < 0 || s.LastWindowEnd < 0 {
		return s, fmt.Errorf("invalid persistent restriction identifiers or counters")
	}
	return s, nil
}

func decodeSecurityAudit(raw json.RawMessage) (sourceSecurityAudit, error) {
	var a sourceSecurityAudit
	if err := securityRequiredFields(raw, "id", "dedupe_key", "source", "decision", "risk_code", "severity", "notification_targets", "notification_success", "review_status"); err != nil {
		return a, err
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, fmt.Errorf("invalid persistent audit fields")
	}
	if a.ID == "" || a.DedupeKey == "" {
		return a, fmt.Errorf("audit ID and dedupe key are required")
	}
	lengths := map[string]int{"id": 64, "dedupe_key": 64, "request_id": 128, "source": 32, "decision": 24, "risk_code": 64,
		"severity": 16, "token_name": 128, "marketplace_channel_id": 64, "marketplace_group_id": 64, "model": 191, "protocol": 96,
		"upstream_error_type": 64, "upstream_error_code": 64, "prompt_hash": 64, "prompt_preview": 512,
		"billing_result": 24, "notification_status": 24, "review_status": 24, "review_note": 1000}
	for name, value := range a.projection() {
		if limit := lengths[name]; limit > 0 {
			var text string
			switch v := value.(type) {
			case string:
				text = v
			case *string:
				if v != nil {
					text = *v
				}
			}
			if utf8.RuneCountInString(text) > limit {
				return a, fmt.Errorf("audit %s exceeds target column length", name)
			}
		}
	}
	return a, nil
}

func securityRequiredFields(raw json.RawMessage, names ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("invalid persistent security row")
	}
	for _, name := range names {
		if value := fields[name]; len(value) == 0 || string(value) == "null" {
			return fmt.Errorf("persistent security field %s must not be null or missing", name)
		}
	}
	return nil
}
