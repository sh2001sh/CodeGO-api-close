//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestFundingIndependentSourceImportPreservesSpendabilityAndHistory(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedFundingFixture(t, source)
	ctx := context.Background()
	reader := readonlySource(t, source)
	importer := NewImporter(reader, target, crypto)
	preview, err := importer.Import(ctx, false)
	if err != nil || len(preview.Issues) > 0 || preview.Applied {
		t.Fatalf("funding preview: %+v %v", preview, err)
	}
	if preview.Counts["retired_features.billing_funding_lots"] != 1 || preview.Counts["retired_features.billing_funding_allocations"] != 1 || preview.Amounts["retired_features.billing_funding_lots.remaining_amount_v2_units"] != "9223372036854775807" {
		t.Fatalf("retired funding exclusion evidence: %+v", preview)
	}
	var count int64
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_billing.funding_lots`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("preview changed target lots count=%d err=%v", count, err)
	}
	for i := 0; i < 2; i++ {
		if report, applyErr := importer.Import(ctx, true); applyErr != nil || !report.Applied {
			t.Fatalf("funding apply%d: %+v %v", i, report, applyErr)
		}
	}
	var balance, lotOriginal, lotRemaining, holdOriginal, holdConsumed, rate, entries int64
	err = target.QueryRow(ctx, `SELECT a.balance,l.original_amount,l.remaining_amount,h.original_amount,h.consumed_amount,
		l.revenue_multiplier_ppm,(SELECT count(*) FROM v3_billing.ledger_entries)
		FROM v3_billing.accounts a JOIN v3_billing.funding_lots l ON l.account_id=a.id
		JOIN v3_billing.wallet_reward_holds h ON h.account_id=a.id
		WHERE l.lot_id='funding-box-lot' AND h.hold_id='funding-hold'`).Scan(&balance, &lotOriginal, &lotRemaining, &holdOriginal, &holdConsumed, &rate, &entries)
	if err != nil || balance != 1000 || lotOriginal != 200 || lotRemaining != 120 || holdOriginal != 200 || holdConsumed != 80 || rate != 650000 || entries != 1 {
		t.Fatalf("funding values balance=%d lot=%d/%d hold=%d/%d rate=%d entries=%d err=%v", balance, lotOriginal, lotRemaining, holdOriginal, holdConsumed, rate, entries, err)
	}
	var excluded bool
	err = target.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM v3_billing.funding_allocations WHERE allocation_id='funding-retired-allocation')
		AND NOT EXISTS(SELECT 1 FROM v3_billing.funding_lots WHERE lot_id='funding-retired-lot')`).Scan(&excluded)
	if err != nil || !excluded {
		t.Fatalf("retired funding exclusion=%t err=%v", excluded, err)
	}
	if _, err = importer.Check(ctx); err != nil {
		t.Fatalf("funding reconciliation: %v", err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_billing.wallet_reward_holds SET consumed_amount=82 WHERE hold_id='funding-hold'`); err != nil {
		t.Fatal(err)
	}
	if report, checkErr := importer.Check(ctx); checkErr == nil || !strings.Contains(fundingReportDetails(report), "transfer hold differs") {
		t.Fatalf("changed transfer hold escaped check: %+v %v", report, checkErr)
	}
	if report, applyErr := importer.Import(ctx, true); applyErr == nil || report.Applied {
		t.Fatalf("reimport overwrote changed hold: %+v %v", report, applyErr)
	}
	if err = source.QueryRow(ctx, `SELECT consumed_amount FROM billing.wallet_reward_holds WHERE hold_id='funding-hold'`).Scan(&holdConsumed); err != nil || holdConsumed != 40 {
		t.Fatalf("source hold modified: %d %v", holdConsumed, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_billing.wallet_reward_holds SET consumed_amount=80 WHERE hold_id='funding-hold'`); err != nil {
		t.Fatal(err)
	}
	if _, err = target.Exec(ctx, `INSERT INTO v3_billing.funding_lots(lot_id,source_account_id,account_id,source,reference_type,reference_id,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,created_at)
		SELECT 'unexpected-lot',source_account_id,account_id,source,reference_type,reference_id,'unexpected-credit',original_amount,remaining_amount,revenue_multiplier_ppm,created_at
		FROM v3_billing.funding_lots WHERE lot_id='funding-box-lot'`); err != nil {
		t.Fatal(err)
	}
	if report, checkErr := importer.Check(ctx); checkErr == nil || !strings.Contains(fundingReportDetails(report), "record count differs") {
		t.Fatalf("extra funding origin escaped count reconciliation: %+v %v", report, checkErr)
	}
}

func fundingReportDetails(report Report) string {
	var out []string
	for _, issue := range report.Issues {
		out = append(out, issue.Detail)
	}
	return strings.Join(out, ";")
}

func TestFundingImportRejectsUndrainedSourcesAndInvalidOrigins(t *testing.T) {
	for _, tc := range []struct {
		name, mutation, code string
	}{
		{"open_reservation", `UPDATE billing.reservations SET status='open'`, "source_not_drained"},
		{"pending_settlement", `UPDATE billing.settlements SET status='pending'`, "source_not_drained"},
		{"pending_outbox", `UPDATE billing.outbox_events SET status='pending'`, "source_not_drained"},
		{"reserved_snapshot", `UPDATE billing.balance_snapshots SET reserved_balance=1`, "source_not_drained"},
		{"broken_lot", `UPDATE billing.funding_allocations SET lot_id='absent' WHERE allocation_id='funding-box-allocation'`, "missing_funding_lot"},
		{"bigint_overflow", `UPDATE billing.funding_lots SET original_amount=4611686018427387904 WHERE lot_id='funding-box-lot'`, "invalid_funding_row"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			seedFundingFixture(t, source)
			ctx := context.Background()
			if _, err := source.Exec(ctx, tc.mutation); err != nil {
				t.Fatal(err)
			}
			importer := NewImporter(readonlySource(t, source), target, crypto)
			report, err := importer.Import(ctx, true)
			found := false
			for _, issue := range report.Issues {
				found = found || issue.Code == tc.code
			}
			if err == nil || report.Applied || !found {
				t.Fatalf("undrained/invalid origin accepted report=%+v err=%v", report, err)
			}
			var count int64
			if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("invalid funding wrote target users count=%d err=%v", count, err)
			}
		})
	}
}

// The read-only transaction itself forbids writes even when its authenticated
// user has privileges, in addition to the fixture's restricted read-only login.
func TestFundingSnapshotIsActuallyReadOnly(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedFundingFixture(t, source)
	ctx := context.Background()
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `UPDATE billing.wallet_reward_holds SET consumed_amount=0`); err == nil {
		t.Fatal("source snapshot accepted a write")
	}
}

func TestFundingRetirementDryRunKeepsOriginalUnitsAndCurrentMoney(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedFundingFixture(t, source)
	ctx := context.Background()
	report, err := NewImporter(readonlySource(t, source), target, crypto).Import(ctx, false)
	if err != nil || len(report.Issues) != 0 || report.Applied {
		t.Fatalf("retired overflow/reservation blocked preview: %+v %v", report, err)
	}
	if report.OpeningMicroCredits != "1000" || report.Counts["billing_funding_lots"] != 1 || report.Counts["billing_funding_allocations"] != 1 || report.Counts["retired_features.billing_funding_lots"] != 1 || report.Amounts["retired_features.billing_funding_lots.remaining_amount_v2_units"] != "9223372036854775807" || report.Amounts["billing_funding_lots_remaining_amount_micro_credits"] != "120" {
		t.Fatalf("retired units entered current money: %+v", report)
	}
	var targetUsers int64
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&targetUsers); err != nil || targetUsers != 0 {
		t.Fatalf("retirement preview wrote target users: %d %v", targetUsers, err)
	}
}
