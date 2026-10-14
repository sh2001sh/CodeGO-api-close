//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func financialVerificationStageChange(t *testing.T, target *pgxpool.Pool, opts OnlineOptions, sql string) {
	t.Helper()
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
		if err := onlineAuthorize(ctx, tx, opts.RunID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, sql)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineVerifyFinancialHistoryOmitsDiagnosticsAndKeepsExactMoney(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	ctx := context.Background()
	onlineCatchupReady(t, m, opts)
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET quota=9007199254740993 WHERE id=51`)
	onlineMigrationSync(t, m, opts)
	financialVerificationStageChange(t, target, opts, `UPDATE `+onlineStage("v3_audit.events")+` SET content='unreviewed ordinary text' WHERE id=51`)
	if _, err := m.VerifyOnline(ctx, opts); err == nil {
		t.Fatal("full history verification accepted different log text")
	}
	opts.FinancialHistoryVerification = true
	r, err := m.VerifyOnlineCatchup(ctx, opts)
	if err != nil || !r.Applied || r.Phase != "verified" || r.HistoryVerification != "financial" {
		t.Fatalf("financial verification=%+v err=%v", r, err)
	}
	var amount, scope string
	if err := target.QueryRow(ctx, "SELECT amount::text FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id=51").Scan(&amount); err != nil || amount != "18014398509481986" {
		t.Fatalf("exact monetary aggregate=%s err=%v", amount, err)
	}
	if err := target.QueryRow(ctx, "SELECT value::text FROM v3_migration_online.totals WHERE name='verification.financial_history'").Scan(&scope); err != nil || scope != "1" {
		t.Fatalf("scope receipt=%s err=%v", scope, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	if _, err := m.FinalizeOnline(ctx, opts); err == nil {
		t.Fatal("financial verification bypassed the source fence")
	}
}

func TestOnlineVerifyFinancialHistoryRejectsMoneyAndFundingDifferences(t *testing.T) {
	for _, item := range []struct{ name, sql, label string }{
		{"usage amount", `UPDATE ` + onlineStage("v3_billing.usage_logs") + ` SET amount=amount+1 WHERE id=51`, "financial history differs"},
		{"same-total different Key", `UPDATE ` + onlineStage("v3_billing.usage_logs") + ` SET key_id=999 WHERE id=51`, "financial history differs"},
		{"funding amount", `UPDATE ` + onlineStage("v3_billing.funding_lots") + ` SET remaining_amount=remaining_amount+1 WHERE lot_id='funding-box-lot'`, "funding projection differs"},
	} {
		t.Run(item.name, func(t *testing.T) {
			m, _, target, opts := onlineMigrationFixture(t, true)
			onlineCatchupReady(t, m, opts)
			financialVerificationStageChange(t, target, opts, item.sql)
			before := onlineRecoveryTotals(t, target)
			opts.FinancialHistoryVerification = true
			r, err := m.VerifyOnlineCatchup(context.Background(), opts)
			if err == nil || !strings.Contains(err.Error(), item.label) || r.Applied || r.Acknowledged != 0 {
				t.Fatalf("money difference accepted=%+v err=%v", r, err)
			}
			if onlineRecoveryTotals(t, target) != before {
				t.Fatal("failed financial verification committed receipts")
			}
			onlineRecoveryAssertNoMoney(t, target)
		})
	}
}

func TestOnlineVerifyFinancialHistoryLateFailureRollsBackScopeReceipt(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	onlineCatchupReady(t, m, opts)
	before := onlineRecoveryTotals(t, target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracer := &onlineCatchupCancelTracer{cancel: cancel}
	config := target.Config().Copy()
	config.ConnConfig.Tracer = tracer
	traced, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer traced.Close()
	clone := *m
	clone.pool = traced
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET quota=57 WHERE id=51`)
	opts.FinancialHistoryVerification = true
	r, err := clone.VerifyOnlineCatchup(ctx, opts)
	if err == nil || r.Applied || r.Acknowledged != 0 || !tracer.fired.Load() {
		t.Fatalf("late financial verification failure=%+v err=%v", r, err)
	}
	if onlineRecoveryTotals(t, target) != before {
		t.Fatal("canceled financial verification committed its scope or amount receipt")
	}
	var pending int64
	if err := source.QueryRow(context.Background(), "SELECT count(*) FROM v3_migration_capture.events WHERE NOT acked").Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("canceled financial verification lost pending changes=%d err=%v", pending, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}
