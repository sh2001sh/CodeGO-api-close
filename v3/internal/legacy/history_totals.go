package legacy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func verifyHistoryTotals(ctx context.Context, target pgx.Tx, d *historyData, report *Report) error {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	if report.Amounts == nil {
		report.Amounts = map[string]string{}
	}
	for _, c := range []struct{ source, table, filter, amount string }{
		{"passkeys_active", "v3_identity.passkeys", " WHERE legacy_id IS NOT NULL", ""},
		{"custom_oauth_providers", "v3_identity.oauth_providers", "", ""},
		{"user_oauth_bindings", "v3_identity.user_identities", " WHERE legacy_binding_id IS NOT NULL", ""},
		{"historical_accounts", "v3_billing.historical_accounts", "", ""},
		{"ledger_entries", "v3_billing.historical_entries", "", "amount"},
		{"logs", "v3_audit.events", "", "amount"},
		{"request_audits", "v3_audit.request_audits", "", "amount"},
		{"request_attempt_audits", "v3_audit.request_attempt_audits", "", ""},
	} {
		var count int64
		query := "SELECT count(*)"
		var sum string
		if c.amount != "" {
			query += ",COALESCE(sum(" + c.amount + "),0)::text"
		}
		query += " FROM " + c.table + c.filter
		row := target.QueryRow(ctx, query)
		var err error
		if c.amount == "" {
			err = row.Scan(&count)
		} else {
			err = row.Scan(&count, &sum)
		}
		if err != nil {
			return err
		}
		report.Counts["check:history:"+c.source+":actual"] = count
		if count != d.counts[c.source] {
			report.Issues = append(report.Issues, Issue{c.source, 0, "history_count_mismatch", fmt.Sprintf("source=%d target=%d", d.counts[c.source], count)})
		}
		if c.amount != "" {
			expected := "0"
			if d.amounts[c.source] != nil {
				expected = d.amounts[c.source].String()
			}
			report.Amounts["check:history:"+c.source+":actual_micro_credits"] = sum
			if expected != sum {
				report.Issues = append(report.Issues, Issue{c.source, 0, "history_amount_mismatch", "typed target amount sum differs from exact source micro-credits"})
			}
		}
	}
	return nil
}
