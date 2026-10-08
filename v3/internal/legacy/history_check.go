package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (m *Importer) checkHistory(ctx context.Context, target pgx.Tx, d *historyData, report *Report) error {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	if report.Amounts == nil {
		report.Amounts = map[string]string{}
	}
	checked := int64(0)
	mappings, err := historyAccountTargets(ctx, target)
	if err != nil {
		return err
	}
	batches := map[string]*historyBatch{}
	check := func(schema, table, key string, id any, projection map[string]json.RawMessage) error {
		fields, err := historyProjectionFields(projection)
		if err != nil {
			return err
		}
		fields[key] = id
		checked++
		name := schema + "." + table
		batch := batches[name]
		if batch == nil {
			keys := []string{key}
			if table == "usage_logs" {
				keys = []string{"created_at", "id"}
			}
			batch = &historyBatch{flush: func(rows []map[string]any) error {
				matches, err := checkExactBulk(ctx, target, schema, table, keys, rows)
				if err != nil {
					return err
				}
				for _, identical := range matches {
					if !identical {
						countKey := "check:history:" + table + ":mismatched"
						report.Counts[countKey]++
						if report.Counts[countKey] == 1 {
							checkIssue(report, table, 0, "typed source row is absent or differs from target")
						}
					}
				}
				return nil
			}}
			batches[name] = batch
		}
		return batch.add(fields)
	}
	for _, p := range d.passkeys {
		var lastUsed any
		if p.LastUsedAt != nil {
			lastUsed = historyDate(*p.LastUsedAt)
		}
		projection, err := historyJSONProjection(map[string]any{"credential_id": p.Credential.ID, "user_id": p.UserID, "credential": p.Credential, "legacy_id": p.ID, "created_at": historyDate(p.CreatedAt), "updated_at": historyDate(p.UpdatedAt), "last_used_at": lastUsed}, nil, nil)
		if err != nil {
			return err
		}
		if err = check("v3_identity", "passkeys", "credential_id", p.Credential.ID, projection); err != nil {
			return err
		}
	}
	for _, p := range d.providers {
		projection, err := historyJSONProjection(p, nil, []string{"client_secret"})
		if err != nil {
			return err
		}
		putHistoryDates(projection, map[string]historyTime{"created_at": p.CreatedAt, "updated_at": p.UpdatedAt})
		if err = check("v3_identity", "oauth_providers", "id", p.ID, projection); err != nil {
			return err
		}
		var ciphertext []byte
		if err = target.QueryRow(ctx, `SELECT secret_ciphertext FROM v3_identity.oauth_providers WHERE id=$1`, p.ID).Scan(&ciphertext); err == pgx.ErrNoRows {
			continue
		} else if err != nil {
			return err
		}
		decrypter, ok := m.crypto.(interface{ Decrypt([]byte) ([]byte, error) })
		if !ok {
			return fmt.Errorf("legacy: checking OAuth secrets requires a decrypter")
		}
		plaintext, decryptErr := decrypter.Decrypt(ciphertext)
		if decryptErr != nil || string(plaintext) != p.ClientSecret {
			report.Issues = append(report.Issues, Issue{"oauth_provider", p.ID, "secret_mismatch", "encrypted custom OAuth secret differs from source"})
		}
	}
	for _, b := range d.bindings {
		projection, err := historyJSONProjection(map[string]any{"user_id": b.UserID, "legacy_binding_id": b.ID, "subject": b.Subject, "created_at": historyDate(b.CreatedAt)}, nil, nil)
		if err != nil {
			return err
		}
		if err = check("v3_identity", "user_identities", "legacy_binding_id", b.ID, projection); err != nil {
			return err
		}
	}
	for _, a := range d.accounts {
		projection, err := historyJSONProjection(a, map[string]string{"account_id": "source_account_id", "quota_unit": "unit", "meta_json": "metadata"}, nil)
		if err != nil {
			return err
		}
		projection["metadata"] = historyMetadata(a.Metadata)
		putHistoryDates(projection, map[string]historyTime{"created_at": a.CreatedAt, "updated_at": a.UpdatedAt})
		var mapped *int64
		owner, kind := historicalAccountMapping(a)
		if id, exists := mappings[historyAccountKey{owner, a.OwnerID, kind}]; exists {
			mapped = &id
		}
		projection["account_id"], _ = json.Marshal(mapped)
		if err = check("v3_billing", "historical_accounts", "source_account_id", a.ID, projection); err != nil {
			return err
		}
	}
	checks := []struct {
		name  string
		visit func(json.RawMessage, bool) error
	}{
		{"ledger_entries", func(raw json.RawMessage, _ bool) error {
			if d.retiredHistoryEntry(raw) {
				return nil
			}
			e, err := decodeHistoryEntry(raw)
			if err != nil {
				return err
			}
			p, err := historyJSONProjection(e, map[string]string{"account_id": "source_account_id"}, nil)
			if err != nil {
				return err
			}
			putHistoryDates(p, map[string]historyTime{"created_at": e.CreatedAt})
			return check("v3_billing", "historical_entries", "entry_id", e.ID, p)
		}},
		{"logs", func(raw json.RawMessage, duplicate bool) error {
			l, err := decodeHistoryLog(raw)
			if err != nil {
				return err
			}
			p, err := historyJSONProjection(l, map[string]string{"type": "event_type", "model_name": "model", "quota": "amount", "use_time": "duration_seconds", "token_id": "key_id", "group": "group_name"}, []string{"other", "Metadata", "CachedTokens"})
			if err != nil {
				return err
			}
			p["metadata"] = l.Metadata
			putHistoryDates(p, map[string]historyTime{"created_at": l.CreatedAt})
			if err = check("v3_audit", "events", "id", l.ID, p); err != nil {
				return err
			}
			if l.Type != 2 {
				return nil
			}
			account, exists := mappings[historyAccountKey{"user", l.UserID, "wallet"}]
			if !exists {
				return fmt.Errorf("legacy: usage log %d has no target user wallet", l.ID)
			}
			request := historyUsageRequestID(l, duplicate)
			u, err := historyJSONProjection(map[string]any{"id": l.ID, "created_at": historyDate(l.CreatedAt), "account_id": account, "user_id": l.UserID, "key_id": l.KeyID, "channel_id": l.ChannelID, "amount": l.Amount, "prompt_tokens": l.PromptTokens, "completion_tokens": l.CompletionTokens, "cached_tokens": l.CachedTokens, "request_id": request, "model": l.Model, "terminal": "completed"}, nil, nil)
			if err != nil {
				return err
			}
			return check("v3_billing", "usage_logs", "id", l.ID, u)
		}},
		{"request_audits", func(raw json.RawMessage, _ bool) error {
			a, err := decodeHistoryRequestAudit(raw)
			if err != nil {
				return err
			}
			p, err := historyJSONProjection(a, map[string]string{"token_id": "key_id", "model_name": "model", "quota": "amount"}, nil)
			if err != nil {
				return err
			}
			putHistoryDates(p, map[string]historyTime{"created_at": a.CreatedAt, "updated_at": a.UpdatedAt, "started_at": a.StartedAt, "completed_at": a.CompletedAt})
			p["completed_at"], _ = json.Marshal(a.completedDate())
			return check("v3_audit", "request_audits", "request_id", a.RequestID, p)
		}},
		{"request_attempt_audits", func(raw json.RawMessage, _ bool) error {
			a, err := decodeHistoryAttemptAudit(raw)
			if err != nil {
				return err
			}
			p, err := historyJSONProjection(a, map[string]string{"model_name": "model"}, nil)
			if err != nil {
				return err
			}
			putHistoryDates(p, map[string]historyTime{"created_at": a.CreatedAt, "started_at": a.StartedAt, "completed_at": a.CompletedAt})
			return check("v3_audit", "request_attempt_audits", "attempt_id", a.AttemptID, p)
		}},
	}
	for _, c := range checks {
		var err error
		if c.name == "logs" {
			err = walkHistoryLogs(ctx, d.source, d.sources[c.name], c.visit)
		} else {
			err = walkHistory(ctx, d.source, d.sources[c.name], func(raw json.RawMessage) error { return c.visit(raw, false) })
		}
		if err != nil {
			return err
		}
	}
	for _, batch := range batches {
		if err := batch.finish(); err != nil {
			return err
		}
	}
	report.Counts["check:history"] = checked
	for name, sum := range d.amounts {
		if strings.HasPrefix(name, "retired:") {
			continue
		}
		report.Amounts["check:history:"+name+":expected_micro_credits"] = sum.String()
	}
	return verifyHistoryTotals(ctx, target, d, report)
}

func historyJSONProjection(value any, renames map[string]string, omit []string) (map[string]json.RawMessage, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if err = json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	for _, key := range omit {
		delete(out, key)
	}
	for old, next := range renames {
		out[next] = out[old]
		delete(out, old)
	}
	return out, nil
}

func putHistoryDates(p map[string]json.RawMessage, dates map[string]historyTime) {
	for name, date := range dates {
		p[name], _ = json.Marshal(historyDate(date))
	}
}

// Decode typed projection values without a float64 round-trip. JSON objects
// stay jsonb; timestamps become time.Time for the shared typed check helper.
func historyProjectionFields(projection map[string]json.RawMessage) (map[string]any, error) {
	fields := map[string]any{}
	for name, raw := range projection {
		switch {
		case string(raw) == "null":
			fields[name] = nil
		case len(raw) > 0 && (raw[0] == '{' || raw[0] == '['):
			fields[name] = raw
		case len(raw) > 0 && raw[0] == '"':
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, err
			}
			if strings.HasSuffix(name, "_at") {
				date, err := time.Parse(time.RFC3339Nano, s)
				if err != nil {
					return nil, err
				}
				fields[name] = date
			} else {
				fields[name] = s
			}
		case string(raw) == "true" || string(raw) == "false":
			fields[name] = string(raw) == "true"
		default:
			var n int64
			if err := json.Unmarshal(raw, &n); err != nil {
				return nil, err
			}
			fields[name] = n
		}
	}
	return fields, nil
}
