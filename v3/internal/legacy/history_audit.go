package legacy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type historyLog struct {
	ID                  int64       `json:"id"`
	UserID              int64       `json:"user_id"`
	CreatedAt           historyTime `json:"created_at"`
	Type                int         `json:"type"`
	Content             string      `json:"content"`
	Username            string      `json:"username"`
	TokenName           string      `json:"token_name"`
	Model               string      `json:"model_name"`
	Amount              int64       `json:"quota"`
	PromptTokens        int64       `json:"prompt_tokens"`
	CompletionTokens    int64       `json:"completion_tokens"`
	DurationSeconds     int64       `json:"use_time"`
	IsStream            bool        `json:"is_stream"`
	ChannelID           int64       `json:"channel_id"`
	KeyID               int64       `json:"token_id"`
	Group               string      `json:"group"`
	IP                  string      `json:"ip"`
	RequestID           string      `json:"request_id"`
	UpstreamRequestID   string      `json:"upstream_request_id"`
	Other               string      `json:"other"`
	Metadata            json.RawMessage
	CachedTokens        int64
	LegacyPromptAnomaly bool `json:"legacy_prompt_anomaly"`
}

func decodeHistoryLog(raw json.RawMessage) (historyLog, error) {
	var l historyLog
	if json.Unmarshal(raw, &l) != nil {
		return l, fmt.Errorf("invalid historical log fields")
	}
	if l.ID <= 0 || l.UserID < 0 || l.Type < 0 || l.Type > 6 {
		return l, fmt.Errorf("invalid historical log ID, user or event type")
	}
	if l.CompletionTokens < 0 || l.DurationSeconds < 0 {
		return l, fmt.Errorf("historical log tokens and duration must be nonnegative")
	}
	amount, err := FromV2Units(l.Amount)
	if err != nil {
		return l, fmt.Errorf("historical log amount overflows micro-credits")
	}
	l.Amount = int64(amount)
	if l.Type == 2 && l.Amount < 0 {
		return l, fmt.Errorf("usage log amount must be nonnegative")
	}
	l.Metadata = json.RawMessage(`{}`)
	l.LegacyPromptAnomaly = false
	if l.Other != "" {
		if !json.Valid([]byte(l.Other)) {
			return l, fmt.Errorf("historical log metadata is invalid JSON")
		}
		l.Metadata = json.RawMessage(l.Other)
		var values map[string]json.RawMessage
		if json.Unmarshal(l.Metadata, &values) == nil {
			for _, name := range []string{"cache_read_tokens", "cached_tokens", "cache_tokens"} {
				if b := values[name]; len(b) > 0 {
					if json.Unmarshal(b, &l.CachedTokens) != nil || l.CachedTokens < 0 {
						return l, fmt.Errorf("invalid historical cached token count")
					}
					break
				}
			}
		}
	}
	if l.PromptTokens < 0 {
		// V2's Anthropic/OpenRouter accounting subtracts cache creation from
		// prompt tokens. Preserve a witnessed historical underflow verbatim;
		// it is neither a reliable token count nor a reason to change the charge.
		var metadata struct {
			Semantic      string `json:"usage_semantic"`
			CacheCreation int64  `json:"cache_creation_tokens"`
		}
		if l.Type != 2 || json.Unmarshal(l.Metadata, &metadata) != nil || metadata.Semantic != "anthropic" ||
			metadata.CacheCreation <= 0 || l.PromptTokens+metadata.CacheCreation < 0 {
			return l, fmt.Errorf("historical negative prompt tokens lack cache subtraction evidence")
		}
		l.LegacyPromptAnomaly = true
	}
	return l, nil
}

func (m *Importer) importHistoryLogs(ctx context.Context, target pgx.Tx, d *historyData) error {
	if onlineViewFrom(ctx) != nil {
		return nil
	}
	mappings, err := historyAccountTargets(ctx, target)
	if err != nil {
		return err
	}
	events := historyImportBatch(ctx, target, "v3_audit", "events", "id")
	usage := historyImportBatch(ctx, target, "v3_billing", "usage_logs", "created_at", "id")
	err = walkHistoryLogs(ctx, d.source, d.sources["logs"], func(raw json.RawMessage, duplicate bool) error {
		l, err := decodeHistoryLog(raw)
		if err != nil {
			return err
		}
		columns := []string{"id", "user_id", "created_at", "event_type", "content", "username", "token_name", "model", "amount", "prompt_tokens", "completion_tokens", "duration_seconds", "is_stream", "channel_id", "key_id", "group_name", "ip", "request_id", "upstream_request_id", "metadata", "legacy_prompt_anomaly"}
		values := []any{l.ID, l.UserID, historyDate(l.CreatedAt), l.Type, l.Content, l.Username, l.TokenName, l.Model, l.Amount, l.PromptTokens, l.CompletionTokens, l.DurationSeconds, l.IsStream, l.ChannelID, l.KeyID, l.Group, l.IP, l.RequestID, l.UpstreamRequestID, l.Metadata, l.LegacyPromptAnomaly}
		if err = events.add(historyFields(columns, values)); err != nil {
			return fmt.Errorf("legacy: import log %d: %w", l.ID, err)
		}
		// Consumption is projected to the same native table as new traffic. All
		// administrative/refund/error records remain available through events.
		if l.Type != 2 {
			return nil
		}
		account, exists := mappings[historyAccountKey{"user", l.UserID, "wallet"}]
		if !exists {
			return fmt.Errorf("legacy: usage log %d has no target user wallet", l.ID)
		}
		requestID := historyUsageRequestID(l, duplicate)
		return usage.add(map[string]any{"id": l.ID, "created_at": historyDate(l.CreatedAt), "account_id": account, "user_id": l.UserID, "key_id": l.KeyID, "channel_id": l.ChannelID, "amount": l.Amount, "prompt_tokens": l.PromptTokens, "completion_tokens": l.CompletionTokens, "cached_tokens": l.CachedTokens, "request_id": requestID, "model": l.Model, "terminal": "completed", "legacy_prompt_anomaly": l.LegacyPromptAnomaly})
	})
	if err != nil {
		return err
	}
	if err = events.finish(); err != nil {
		return err
	}
	return usage.finish()
}

func historyUsageRequestID(l historyLog, duplicate bool) string {
	if duplicate && l.RequestID != "" {
		return fmt.Sprintf("%s:v2-log:%d", l.RequestID, l.ID)
	}
	if l.RequestID != "" {
		return l.RequestID
	}
	return fmt.Sprintf("v2-log:%d", l.ID)
}

// The reviewed 107 -> 108 upgrade deliberately excludes later revisions.
// Preserve that supported path for ordinary logs, but require the new schema
// for anomalies instead of silently changing its signed evidence.
func historyPromptAnomalySchema(ctx context.Context, target pgx.Tx) (bool, error) {
	var columns int
	err := target.QueryRow(ctx, `SELECT count(*) FROM pg_attribute
 WHERE attrelid IN ('v3_audit.events'::regclass,'v3_billing.usage_logs'::regclass)
 AND attname='legacy_prompt_anomaly' AND NOT attisdropped`).Scan(&columns)
	if err == nil && columns == 1 {
		err = fmt.Errorf("legacy: historical prompt anomaly schema is incomplete")
	}
	return columns == 2, err
}

func historyPromptAnomalyFields(l historyLog, supported bool, fields map[string]any) error {
	if !supported {
		if l.LegacyPromptAnomaly {
			return fmt.Errorf("legacy: historical prompt anomaly requires schema 110 and a fresh target")
		}
		delete(fields, "legacy_prompt_anomaly")
	}
	return nil
}

func (m *Importer) importHistory(ctx context.Context, target pgx.Tx, d *historyData) error {
	if d == nil {
		return nil
	}
	if len(d.issues) > 0 {
		return fmt.Errorf("legacy: history import has unresolved validation issues")
	}
	if err := m.importHistoryIdentity(ctx, target, d); err != nil {
		return err
	}
	if err := m.importHistoryLedger(ctx, target, d); err != nil {
		return err
	}
	if err := m.importHistoryLogs(ctx, target, d); err != nil {
		return err
	}
	if err := m.importHistoryRequestAudits(ctx, target, d); err != nil {
		return err
	}
	if err := importRetiredUsageTotals(ctx, target, d.retiredUsage); err != nil {
		return err
	}
	r := Report{}
	if err := verifyHistoryTotals(ctx, target, d, &r); err != nil {
		return err
	}
	if len(r.Issues) > 0 {
		return fmt.Errorf("legacy: history count or amount verification failed: %s", r.Issues[0].Detail)
	}
	return nil
}
