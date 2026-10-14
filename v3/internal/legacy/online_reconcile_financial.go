package legacy

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
)

type financialReconciliationKey struct{}

func financialReconciliation(ctx context.Context, name string) bool {
	enabled, _ := ctx.Value(financialReconciliationKey{}).(bool)
	return enabled && aggregateHistorySource(name)
}

// Compare financial fields without reading ordinary diagnostic payloads. A row
// needing repair is reread in full from the same source snapshot before replay.
func financialReconcileSource(name string) []string {
	switch name {
	case "logs":
		return []string{"id", "user_id", "created_at", "type", "quota", "token_id", "request_id", "channel_id", "model_name"}
	case "request_audits":
		return []string{"request_id", "user_id", "token_id", "quota", "created_at", "model_name", "final_channel_id"}
	case "request_attempt_audits":
		return []string{"attempt_id", "request_id", "created_at"}
	}
	return nil
}

func financialReconcilePayload(name string) string {
	var fields []string
	for _, column := range financialReconcileSource(name) {
		fields = append(fields, "'"+column+"'", "t."+pgx.Identifier{column}.Sanitize())
	}
	return "jsonb_build_object(" + strings.Join(fields, ",") + ")"
}

func financialReconcileProjection(row onlineProjection) onlineProjection {
	columns := append([]string(nil), row.Keys...)
	switch row.Schema + "." + row.Table {
	case "v3_audit.events":
		columns = append(columns, "user_id", "created_at", "event_type", "amount", "key_id", "request_id", "channel_id", "model")
	case "v3_billing.usage_logs":
		columns = append(columns, "account_id", "user_id", "key_id", "channel_id", "amount", "request_id", "model", "terminal")
	case "v3_audit.request_audits":
		columns = append(columns, "user_id", "key_id", "amount", "created_at", "model", "final_channel_id")
	case "v3_audit.request_attempt_audits":
		columns = append(columns, "request_id", "created_at")
	case "v3_audit.orphan_request_attempt_history":
		columns = append(columns, "request_id")
	}
	fields := make(map[string]any, len(columns))
	for _, column := range columns {
		if value, exists := row.Values[column]; exists {
			fields[column] = value
		}
	}
	row.Values = fields
	return row
}

func (p *onlineProjector) repairFinancialReconcilePage(ctx context.Context, source pgx.Tx, spec onlineSpec, inputs []onlineInput) error {
	keys := make([]json.RawMessage, len(inputs))
	for i, input := range inputs {
		keys[i] = input.key
	}
	// Preserve the existing wide-page memory limit across full rereads and the
	// target commit. The coordinator releases this token after commit/rollback.
	if !p.reconcileReadHeld {
		if err := p.acquireReconcileRead(ctx); err != nil {
			return err
		}
	}
	return onlineCurrentBatches(ctx, source, spec, keys, func(full []onlineInput) error {
		return p.apply(ctx, spec, full)
	})
}
