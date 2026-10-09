//go:build pgintegration

package legacy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type onlineRejectQueueScanTx struct {
	pgx.Tx
	scans int
}

var errOnlineQueueScan = errors.New("historical queue scan forbidden in projection")

func (tx *onlineRejectQueueScanTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "GROUP BY status") {
		tx.scans++
		return nil, errOnlineQueueScan
	}
	return tx.Tx.Query(ctx, sql, args...)
}

func TestOnlineProjectorAvoidsQueueScansAndFinalFundingStillRequiresThem(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	ctx := context.Background()
	if _, err := m.PrepareOnline(ctx, opts, true); err != nil {
		t.Fatal(err)
	}
	read, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	sources, err := discoverSources(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	write, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = write.Rollback(ctx) }()
	if err := onlineAuthorize(ctx, write, opts.RunID); err != nil {
		t.Fatal(err)
	}
	guarded := &onlineRejectQueueScanTx{Tx: read}
	p, err := loadOnlineProjector(ctx, guarded, write, sources)
	if err != nil || len(p.funding.users) == 0 || len(p.funding.accounts) == 0 || guarded.scans != 0 {
		t.Fatalf("online dependencies require historical queues: scans=%d err=%v", guarded.scans, err)
	}
	if _, err := loadFunding(ctx, guarded, sources); !errors.Is(err, errOnlineQueueScan) || guarded.scans != 1 {
		t.Fatalf("final funding silently bypassed queue validation: scans=%d err=%v", guarded.scans, err)
	}
}

func TestOnlineRebuildTotalsPreservesLargeSignedNullEmptyAndRetiredAmounts(t *testing.T) {
	_, target, _ := importTestDB(t)
	ctx := context.Background()
	write, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = write.Rollback(ctx) }()
	for _, sql := range []string{
		"CREATE SCHEMA v3_migration_online",
		"CREATE TABLE v3_migration_online.totals(name text PRIMARY KEY,value numeric NOT NULL)",
		"CREATE TABLE " + onlineStage("v3_billing.scan_cost") + "(id bigint,amount bigint,remaining_amount bigint)",
		"CREATE TABLE " + onlineStage("v3_billing.scan_empty") + "(amount bigint)",
		"INSERT INTO " + onlineStage("v3_billing.scan_cost") + " VALUES(1,9223372036854775807,NULL),(2,9223372036854775807,-3),(3,NULL,3)",
		"INSERT INTO v3_migration_online.totals VALUES('stale.amount',123),('retired.history.retired:ledger_entries.amount_v2_units',7),('retired_features.billing_funding_allocations.amount_v2_units',9)",
	} {
		if _, err := write.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	specs := []onlineSpec{{targets: []string{"v3_billing.scan_cost", "v3_billing.scan_empty"}}}
	if err := onlineRebuildTotals(ctx, write, specs); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"v3_billing.scan_cost.rows":                                    "3",
		"v3_billing.scan_cost.amount":                                  "18446744073709551614",
		"v3_billing.scan_cost.remaining_amount":                        "0",
		"v3_billing.scan_empty.rows":                                   "0",
		"v3_billing.scan_empty.amount":                                 "0",
		"retired.history.retired:ledger_entries.amount_v2_units":       "7",
		"retired_features.billing_funding_allocations.amount_v2_units": "9",
	}
	rows, err := write.Query(ctx, "SELECT name,value::text FROM v3_migration_online.totals")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if expected, exists := want[name]; !exists || value != expected {
			rows.Close()
			t.Fatalf("incorrect independent total %s=%s expected=%s", name, value, expected)
		}
		delete(want, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(want) != 0 {
		t.Fatalf("missing independent totals: %v err=%v", want, err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// A failure after the initial deletion must not publish partial receipts.
	before := onlineRecoveryTotals(t, target)
	failed, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := onlineRebuildTotals(ctx, failed, []onlineSpec{{targets: []string{"v3_billing.scan_missing"}}}); err == nil {
		_ = failed.Rollback(ctx)
		t.Fatal("missing stage unexpectedly accepted")
	}
	if err := failed.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if after := onlineRecoveryTotals(t, target); before != after {
		t.Fatalf("failed rebuild escaped rollback before=%s after=%s", before, after)
	}
}

func TestOnlineSyncKeepsUnchangedAccountsAndRefreshesCapturedMetadata(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	ctx := context.Background()
	onlineRecoveryExec(t, source, "ALTER TABLE billing.accounts ADD COLUMN meta_json jsonb NOT NULL DEFAULT '{}'::jsonb")
	onlineMigrationReady(t, m, opts)
	accountVersions := func() string {
		t.Helper()
		var value string
		if err := target.QueryRow(ctx, "SELECT md5(string_agg(source_account_id||':'||xmin::text,',' ORDER BY source_account_id)) FROM "+onlineStage("v3_billing.historical_accounts")).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := accountVersions()
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET content='log-only change' WHERE id=51")
	onlineMigrationSync(t, m, opts)
	if after := accountVersions(); before != after {
		t.Fatal("log-only event rewrote historical accounts")
	}
	onlineRecoveryExec(t, source, `UPDATE billing.accounts SET meta_json='{"note":"captured account update"}'::jsonb WHERE account_id='wallet-7'`)
	onlineMigrationSync(t, m, opts)
	var note string
	if err := target.QueryRow(ctx, "SELECT metadata->>'note' FROM "+onlineStage("v3_billing.historical_accounts")+" WHERE source_account_id='wallet-7'").Scan(&note); err != nil || note != "captured account update" {
		t.Fatalf("captured account metadata was lost: note=%s err=%v", note, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
}
