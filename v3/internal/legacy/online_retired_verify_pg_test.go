//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestOnlineVerifyIndependentlyChecksRetiredReceipts(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	seedRetiredHistoryFixture(t, source)
	ctx := context.Background()
	if r, err := m.PrepareOnline(ctx, opts, true); err != nil || !r.Applied {
		t.Fatalf("prepare=%+v err=%v", r, err)
	}
	if r, err := m.CopyOnline(ctx, opts); err != nil || r.Phase != "copied" {
		t.Fatalf("copy=%+v err=%v", r, err)
	}
	onlineMigrationSync(t, m, opts)
	original := map[string]string{}
	rows, err := target.Query(ctx, "SELECT name,value::text FROM v3_migration_online.totals WHERE name LIKE 'retired.%' OR name LIKE 'retired_features.%'")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		original[name] = value
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if original["retired.history.retired:ledger_entries.amount_v2_units"] != "18446744073709551614" || original["retired_features.billing_funding_lots.remaining_amount_v2_units"] != "9223372036854775807" {
		t.Fatalf("fixture lacks exact large retired sums: %+v", original)
	}
	if _, err := target.Exec(ctx, "UPDATE v3_migration_online.totals SET value=value+1 WHERE name='retired.history.retired:ledger_entries'"); err == nil {
		t.Fatal("ordinary writer could mutate a retired verification receipt")
	}
	mutate := func(key, value string, remove bool) {
		t.Helper()
		if err := pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
			if err := onlineAuthorize(ctx, tx, opts.RunID); err != nil {
				return err
			}
			if remove {
				_, err := tx.Exec(ctx, "DELETE FROM v3_migration_online.totals WHERE name=$1", key)
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO v3_migration_online.totals(name,value) VALUES($1,$2::numeric) ON CONFLICT(name)DO UPDATE SET value=EXCLUDED.value", key, value)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, key, value string
		remove           bool
	}{
		{"history_count_changed", "retired.history.retired:ledger_entries", "3", false},
		{"history_sum_changed", "retired.history.retired:ledger_entries.amount_v2_units", "18446744073709551615", false},
		{"funding_count_changed", "retired_features.billing_funding_lots", "2", false},
		{"funding_sum_changed", "retired_features.billing_funding_lots.remaining_amount_v2_units", "9223372036854775808", false},
		{"history_count_missing", "retired.history.retired:ledger_entries", "", true},
		{"funding_sum_missing", "retired_features.billing_funding_allocations.amount_v2_units", "", true},
		{"unexpected_history_zero", "retired.history.fabricated", "0", false},
		{"unexpected_funding_zero", "retired_features.billing_funding_lots.fabricated", "0", false},
		{"fractional_count", "retired.history.retired:ledger_entries", "2.5", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutate(tc.key, tc.value, tc.remove)
			if r, err := m.VerifyOnline(ctx, opts); err == nil || r.Applied || !strings.Contains(err.Error(), "retired receipt") {
				t.Fatalf("invalid retired receipt accepted result=%+v err=%v", r, err)
			}
			var phase string
			if err := target.QueryRow(ctx, "SELECT phase FROM v3_migration_online.run").Scan(&phase); err != nil || phase == "verified" {
				t.Fatalf("failed verify accepted run phase=%s err=%v", phase, err)
			}
			before, exists := original[tc.key]
			mutate(tc.key, before, !exists)
		})
	}
	if r, err := m.VerifyOnline(ctx, opts); err != nil || r.Phase != "verified" {
		t.Fatalf("untampered retired receipt refused: %+v err=%v", r, err)
	}
	// A legitimate deletion leaves known metric keys at zero in the incremental
	// totals. The source has no excluded rows, so these exact zeroes remain valid.
	if _, err := source.Exec(ctx, `DELETE FROM billing.ledger_entries WHERE account_id IN ('retired-points','retired-gpt');
	 DELETE FROM billing.funding_allocations WHERE account_id='retired-gpt-7';
	 DELETE FROM billing.funding_lots WHERE account_id='retired-gpt-7'`); err != nil {
		t.Fatal(err)
	}
	onlineMigrationSync(t, m, opts)
	if r, err := m.VerifyOnline(ctx, opts); err != nil || r.Phase != "verified" {
		t.Fatalf("legitimate retired deletion refused: %+v err=%v", r, err)
	}
}
