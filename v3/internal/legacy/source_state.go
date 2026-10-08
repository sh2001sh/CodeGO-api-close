package legacy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// A stopped writer can leave durable work pending. Offline migration must
// reject it rather than lose a future debit, refund or provider response.
func validateSourceState(ctx context.Context, source pgx.Tx, sources map[string]string, keys []json.RawMessage, report *Report) error {
	guards := []struct {
		name, alias, field string
		terminal           []string
	}{
		{"reservations", "billing_reservations", "status", []string{"settled", "released", "expired"}},
		{"settlements", "billing_settlements", "status", []string{"completed", "rejected"}},
		{"outbox_events", "billing_outbox_events", "status", []string{"published"}},
		{"responses_background_jobs", "gateway_responses_background_jobs", "status", []string{"completed", "failed", "cancelled"}},
		{"tasks", "tasks", "status", []string{"SUCCESS", "FAILURE"}},
		{"task_workflows", "workflow_task_workflows", "status", []string{"succeeded", "failed", "timeout"}},
		{"task_terminal_results", "workflow_task_terminal_results", "settlement_status", []string{"settled", "refunded"}},
	}
	for _, guard := range guards {
		table := sources[guard.alias]
		if table == "" {
			table = sources[guard.name]
		}
		if table == "" {
			continue
		}
		var pending int64
		field := pgx.Identifier{guard.field}.Sanitize()
		if err := source.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE coalesce(`+field+`,'')<>ALL($1::text[])`, guard.terminal).Scan(&pending); err != nil {
			return fmt.Errorf("legacy: inspect in-flight %s: %w", guard.name, err)
		}
		report.Counts["in_flight:"+guard.alias] = pending
		if pending > 0 {
			report.Issues = append(report.Issues, Issue{guard.alias, 0, "source_work_pending", "finish or cancel pending source work before stopping writers"})
		}
	}
	if err := validateProjectionDrains(ctx, source, sources, report); err != nil {
		return err
	}
	if sources["accounts"] == "" {
		return nil
	}
	if err := validateKeyBudgetMirrors(ctx, source, sources, report); err != nil {
		return err
	}
	keyMap := map[int64]sourceKey{}
	for _, row := range keys {
		key, _, _, err := decodeKey(row)
		if err == nil {
			keyMap[key.ID] = key
		}
	}
	rows, err := source.Query(ctx, `SELECT a.owner_type,a.owner_id,a.account_type,a.quota_unit,
		s.available_balance,s.reserved_balance FROM `+sources["accounts"]+` a
		LEFT JOIN `+sources["balance_snapshots"]+` s ON s.account_id=a.account_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var owner, kind, unit string
		var id int64
		var balance, reserved *int64
		if err = rows.Scan(&owner, &id, &kind, &unit, &balance, &reserved); err != nil {
			return err
		}
		issue := func(code, detail string) { report.Issues = append(report.Issues, Issue{"account", id, code, detail}) }
		if retiredAccountKind(kind) {
			report.Counts["retired_features.billing_accounts"]++
			addRetiredAmount(report, "billing_accounts.available_v2_units", balance)
			addRetiredAmount(report, "billing_accounts.reserved_v2_units", reserved)
			continue
		}
		if balance == nil || reserved == nil {
			issue("missing_account_snapshot", "canonical account has no current balance snapshot")
			continue
		}
		if *reserved != 0 {
			issue("open_account_reservations", "canonical account still contains reserved credits")
		}
		if _, err := OpeningBalance(*balance); err != nil {
			issue("invalid_account_balance", err.Error())
		}
		known := unit == "quota" && ((owner == "user" && (kind == "claude_wallet" || kind == "marketplace_owner_pending")) ||
			(owner == "token" && kind == "token") || (owner == "user_subscription" && kind == "subscription") ||
			(owner == "system" && kind == "marketplace_platform_revenue"))
		if !known && (*balance != 0 || *reserved != 0) {
			issue("unmapped_active_account", "nonzero account has no native funding mapping")
		}
		if owner == "token" && kind == "token" {
			_, exists := keyMap[id]
			if !exists {
				issue("missing_token_account_owner", "canonical key account references an absent API key")
			}
		}
	}
	return rows.Err()
}
