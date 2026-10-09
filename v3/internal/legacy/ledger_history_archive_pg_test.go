//go:build pgintegration

package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ledgerArchiveSourceDigest(t *testing.T, source *pgxpool.Pool) string {
	t.Helper()
	var digest string
	if err := source.QueryRow(context.Background(), `SELECT jsonb_agg(to_jsonb(t) ORDER BY entry_id)::text FROM billing.ledger_entries t`).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestLedgerHistoryArchiveOfflinePreservesFundingUsageAndIdentity(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(map[bool]string{false: "default_copy", true: "archive"}[archived], func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			private := historyFixture(t, source)
			seedFundingFixture(t, source)
			seedRetiredHistoryFixture(t, source)
			before := ledgerArchiveSourceDigest(t, source)
			ctx := context.Background()
			m := NewImporter(readonlySource(t, source), target, crypto).WithLedgerHistoryArchive(archived)
			preview, err := m.Import(ctx, false)
			if err != nil || preview.Applied || len(preview.Issues) > 0 {
				t.Fatalf("preview=%+v err=%v", preview, err)
			}
			var metadata int64
			if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.ledger_history_archive").Scan(&metadata); err != nil || metadata != 0 {
				t.Fatalf("preview wrote archive metadata=%d err=%v", metadata, err)
			}
			for i := 0; i < 2; i++ {
				if report, err := m.Import(ctx, true); err != nil || !report.Applied || len(report.Issues) > 0 {
					t.Fatalf("import%d=%+v err=%v", i, report, err)
				}
			}
			checked, err := m.Check(ctx)
			if err != nil || len(checked.Issues) != 0 {
				t.Fatalf("check=%+v err=%v", checked, err)
			}
			var historical, balance, entries, events, usage, usageAmount, requests, attempts, lotOriginal, lotRemaining, holdConsumed int64
			err = target.QueryRow(ctx, `SELECT
			 (SELECT count(*) FROM v3_billing.historical_entries),
			 (SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'),
			 (SELECT count(*) FROM v3_billing.ledger_entries),
			 (SELECT count(*) FROM v3_audit.events),(SELECT count(*) FROM v3_billing.usage_logs),
			 (SELECT sum(amount) FROM v3_billing.usage_logs),(SELECT count(*) FROM v3_audit.request_audits),
			 (SELECT count(*) FROM v3_audit.request_attempt_audits),
			 (SELECT original_amount FROM v3_billing.funding_lots WHERE lot_id='funding-box-lot'),
			 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE lot_id='funding-box-lot'),
			 (SELECT consumed_amount FROM v3_billing.wallet_reward_holds WHERE hold_id='funding-hold')`).Scan(
				&historical, &balance, &entries, &events, &usage, &usageAmount, &requests, &attempts, &lotOriginal, &lotRemaining, &holdConsumed)
			wantHistorical := int64(2)
			if archived {
				wantHistorical = 0
			}
			if err != nil || historical != wantHistorical || balance != 1000 || entries != 1 || events != 4 || usage != 2 || usageAmount != 120 || requests != 1 || attempts != 1 || lotOriginal != 200 || lotRemaining != 120 || holdConsumed != 80 {
				t.Fatalf("history=%d wallet=%d/%d audit=%d/%d/%d/%d usageAmount=%d funding=%d/%d/%d err=%v", historical, balance, entries, events, usage, requests, attempts, usageAmount, lotOriginal, lotRemaining, holdConsumed, err)
			}
			if checked.Amounts["retired_features.billing_funding_lots.remaining_amount_v2_units"] != "9223372036854775807" {
				t.Fatalf("funding retirement evidence was lost: %+v", checked)
			}
			if archived {
				a := checked.LedgerHistoryArchive
				if a == nil || a.EntryCount != 4 || a.SourceClusterID == "" || a.SourceDatabaseOID <= 0 || a.SourceDatabase != source.Config().ConnConfig.Database || a.ArchivedAt.IsZero() || checked.Counts["history.ledger_entries"] != 0 {
					t.Fatalf("archive proof=%+v report=%+v", a, checked)
				}
			} else if checked.LedgerHistoryArchive != nil || checked.Counts["history.ledger_entries"] != 2 {
				t.Fatalf("default copy mode changed: %+v", checked)
			}
			other := NewImporter(m.source, target, crypto).WithLedgerHistoryArchive(!archived)
			if report, err := other.Import(ctx, true); err == nil || report.Applied {
				t.Fatalf("opposite mode overwrote target: %+v err=%v", report, err)
			}
			if report, err := other.Check(ctx); err == nil {
				t.Fatalf("opposite mode accepted target: %+v", report)
			}
			if after := ledgerArchiveSourceDigest(t, source); after != before {
				t.Fatal("migration or mode refusal changed original active/retired source ledger")
			}
			if report, err := m.Check(ctx); err != nil || len(report.Issues) > 0 {
				t.Fatalf("mode refusal did not roll back: %+v err=%v", report, err)
			}
			assertHistoryPasskeyLogin(t, target, private)
		})
	}
}

func TestLedgerHistoryArchiveRejectsProofAndCopiedHistoryTampering(t *testing.T) {
	source, target, crypto := importTestDB(t)
	historyFixture(t, source)
	seedRetiredHistoryFixture(t, source)
	ctx := context.Background()
	m := NewImporter(readonlySource(t, source), target, crypto).WithLedgerHistoryArchive(true)
	if report, err := m.Import(ctx, true); err != nil || !report.Applied {
		t.Fatalf("import=%+v err=%v", report, err)
	}
	for _, tc := range []struct{ name, mutation, restore string }{
		{"source_count", `INSERT INTO billing.ledger_entries SELECT 'extra-source',account_id,reference_type,reference_id,entry_type,direction,amount,balance_after,'extra-source-key',reason_code,reason_detail,operator_type,operator_id,metadata,created_at FROM billing.ledger_entries WHERE entry_id='credit-original'`, `DELETE FROM billing.ledger_entries WHERE entry_id='extra-source'`},
		{"cluster", `UPDATE v3_billing.ledger_history_archive SET source_cluster_id=source_cluster_id||'changed'`, `UPDATE v3_billing.ledger_history_archive SET source_cluster_id=left(source_cluster_id,length(source_cluster_id)-7)`},
		{"database_oid", `UPDATE v3_billing.ledger_history_archive SET source_database_oid=source_database_oid+1`, `UPDATE v3_billing.ledger_history_archive SET source_database_oid=source_database_oid-1`},
		{"metadata_count", `UPDATE v3_billing.ledger_history_archive SET entry_count=entry_count+1`, `UPDATE v3_billing.ledger_history_archive SET entry_count=entry_count-1`},
		{"copied_history", `INSERT INTO v3_billing.historical_entries VALUES('forged','wallet-7','request','forged','credit','credit',1,NULL,'forged','','','system','','{}','2024-01-01T00:00:00Z')`, `DELETE FROM v3_billing.historical_entries WHERE entry_id='forged'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := target
			if tc.name == "source_count" {
				pool = source
			}
			if _, err := pool.Exec(ctx, tc.mutation); err != nil {
				t.Fatal(err)
			}
			if report, err := m.Check(ctx); err == nil || !strings.Contains(err.Error(), "archive") {
				t.Fatalf("tampered archive accepted: %+v err=%v", report, err)
			}
			if report, err := m.Import(ctx, true); err == nil || report.Applied {
				t.Fatalf("tampered archive overwritten: %+v err=%v", report, err)
			}
			if _, err := pool.Exec(ctx, tc.restore); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Archive mode keeps the complete source validation and target reconciliation
	// of the histories which still feed usage statistics and current funds.
	if _, err := target.Exec(ctx, "UPDATE v3_billing.usage_logs SET amount=amount+1 WHERE id=51"); err != nil {
		t.Fatal(err)
	}
	if report, err := m.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatalf("usage tampering escaped archive check: %+v err=%v", report, err)
	}
	if _, err := target.Exec(ctx, "UPDATE v3_billing.usage_logs SET amount=amount-1 WHERE id=51"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, "UPDATE billing.balance_snapshots SET reserved_balance=1 WHERE account_id='wallet-7'"); err != nil {
		t.Fatal(err)
	}
	if report, err := m.Import(ctx, true); err == nil || report.Applied || len(report.Issues) == 0 {
		t.Fatalf("undrained source escaped archive import: %+v err=%v", report, err)
	}
	var openings int64
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='opening'").Scan(&openings); err != nil || openings != 1 {
		t.Fatalf("refused archive replay changed money=%d err=%v", openings, err)
	}
}

func TestLedgerHistoryArchivePreservesRenewableLedgerEvidence(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCommerceFixture(t, source)
	seedMarketplaceFixture(t, source)
	seedFundingFixture(t, source)
	historyFixture(t, source)
	ctx := context.Background()
	_, err := source.Exec(ctx, `INSERT INTO migration_source.options(key,value) VALUES('QuotaPerUnit','250000');
	 UPDATE migration_source.subscription_plans SET total_amount=100000;
	 UPDATE migration_source.user_subscriptions SET amount_total=1000000,amount_used=900000,period_amount=400000,period_used=350000 WHERE id=9;
	 UPDATE billing.balance_snapshots SET available_balance=100000 WHERE account_id='subscription-9';
	 UPDATE legacy_boxes.group_buy_members SET bonus_amount_usd=0.400002,created_at=1700000000 WHERE id=100;
	 INSERT INTO migration_source.subscription_orders(id,user_id,plan_id,money,trade_no,payment_method,payment_provider,status,fulfillment_status,create_time,complete_time,purchase_type,target_subscription_id,group_buy_id,fuel_quota,fuel_unit_price,fuel_expires_at,original_money,first_purchase_discount_applied,first_purchase_discount_multiplier,provider_payload) VALUES
	 (3,7,5,12.12,'prior-group-11','alipay','epay','success','completed',1699999900,1699999901,'group_buy',9,11,0,0,0,12.12,false,0,'{}'),
	 (4,7,5,12.12,'prior-group-12','alipay','epay','success','completed',1699999900,1699999901,'group_buy',9,12,0,0,0,12.12,false,0,'{}');
	 INSERT INTO legacy_boxes.group_buy_orders(id,initiator_id,plan_id,target_count,current_count,status,expires_at,settled_at,created_at,updated_at) VALUES
	 (11,7,5,5,1,'pending',1702592000,0,1699999900,1700000001),(12,7,5,5,1,'pending',1702592000,0,1699999900,1699999950);
	 INSERT INTO legacy_boxes.group_buy_members(id,group_buy_id,user_id,order_id,user_subscription_id,bonus_granted,bonus_amount_usd,created_at) VALUES
	 (102,11,7,3,9,true,0.30,1699999900),(104,12,7,4,9,true,0.50,1699999900);
	 INSERT INTO billing.ledger_entries(entry_id,account_id,reference_type,reference_id,entry_type,direction,amount,balance_after,idempotency_key,reason_code,reason_detail,operator_type,operator_id,metadata,created_at) VALUES
	 ('delayed-group','subscription-9','group_buy','11','grant','credit',150000,NULL,'group-buy:11:member:102:tier:150000','subscription_bonus','','system','','{}','2023-11-14T22:13:20Z'),
	 ('old-group','subscription-9','group_buy','12','grant','credit',250000,NULL,'group-buy:12:member:104:tier:250000','subscription_bonus','','system','','{}','2023-11-14T22:13:19Z'),
	 ('fuel-credit','subscription-9','fuel','old-fuel','grant','credit',500000,NULL,'fuel:old-fuel','subscription_fuel','','system','','{}','2023-11-14T22:13:20Z')`)
	if err != nil {
		t.Fatal(err)
	}
	m := NewImporter(readonlySource(t, source), target, crypto).WithLedgerHistoryArchive(true)
	if report, err := m.Import(ctx, true); err != nil || !report.Applied {
		t.Fatalf("import=%+v err=%v", report, err)
	}
	if report, err := m.Check(ctx); err != nil || len(report.Issues) != 0 {
		t.Fatalf("check=%+v err=%v", report, err)
	}
	var renewable, balance, used, historical, evidence int64
	err = target.QueryRow(ctx, `SELECT s.renewable_credits,a.balance,s.used_credits,
	 (SELECT count(*) FROM v3_billing.historical_entries)
	 FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id WHERE s.id=9`).Scan(&renewable, &balance, &used, &historical)
	if err != nil || renewable != 700002 || balance != 100000 || used != 1800000 || historical != 0 {
		t.Fatalf("renewable=%d balance=%d used=%d history=%d err=%v", renewable, balance, used, historical, err)
	}
	if err := source.QueryRow(ctx, "SELECT count(*) FROM billing.ledger_entries WHERE account_id='subscription-9'").Scan(&evidence); err != nil || evidence != 3 {
		t.Fatalf("subscription ledger evidence=%d err=%v", evidence, err)
	}
	// A changed historical grant must still change the source-derived entitlement
	// expectation even when the archive row count is unchanged.
	if _, err := source.Exec(ctx, "UPDATE billing.ledger_entries SET amount=150001 WHERE entry_id='delayed-group'"); err != nil {
		t.Fatal(err)
	}
	if report, err := m.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatalf("ledger-based entitlement proof was bypassed: %+v err=%v", report, err)
	}
}

// The real base dump and delta are compared table by table in an isolated
// online_restore_ database. No projection filter may truncate backup coverage.
func ledgerArchivePrepareRestore(t *testing.T, source *pgxpool.Pool, runID string) func(*testing.T) {
	t.Helper()
	for _, tool := range []string{"pg_dump", "pg_restore"} {
		if _, err := exec.LookPath(tool); err != nil {
			return func(t *testing.T) { t.Skip("real archive recovery requires pg_dump and pg_restore on PATH") }
		}
	}
	ctx, _, restored, opts := onlineBackupDBs(t)
	opts.RunID = runID
	dump := filepath.Join(t.TempDir(), "archive-base.dump")
	manifest, err := OnlineBaseBackup(ctx, source, runID, dump)
	if err != nil || manifest.Bytes == 0 || len(manifest.SHA256) != 64 {
		t.Fatalf("base backup=%+v err=%v", manifest, err)
	}
	list, err := exec.CommandContext(ctx, "pg_restore", "--list", dump).Output()
	if err != nil {
		t.Fatal(err)
	}
	var keep []string
	for _, line := range strings.Split(string(list), "\n") {
		excluded := false
		if strings.Contains(line, " TRIGGER ") {
			for _, trigger := range manifest.ExcludedTriggerNames {
				if strings.Contains(line, " "+trigger+" ") {
					excluded = true
				}
			}
		}
		if !excluded {
			keep = append(keep, line)
		}
	}
	listPath := filepath.Join(t.TempDir(), "restore.list")
	if err := os.WriteFile(listPath, []byte(strings.Join(keep, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	env, err := onlineDumpEnvironment(restored.Config().ConnConfig)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "pg_restore", "--exit-on-error", "--use-list="+listPath, "--dbname="+opts.Database, dump)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PG") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, env...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("base restore failed: %v %s", err, output)
	}
	return func(t *testing.T) {
		t.Helper()
		var delta bytes.Buffer
		if _, err := ExportOnlineDelta(ctx, source, runID, &delta); err != nil {
			t.Fatal(err)
		}
		var header onlineDeltaRecord
		if err := json.Unmarshal(bytes.SplitN(delta.Bytes(), []byte{'\n'}, 2)[0], &header); err != nil {
			t.Fatal(err)
		}
		if _, err := ApplyOnlineDelta(ctx, restored, &delta, opts); err != nil {
			t.Fatal(err)
		}
		for _, table := range header.Tables {
			var want, actual string
			query := "SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]')::text FROM " + table.Relation + " t"
			if err := source.QueryRow(ctx, query).Scan(&want); err != nil {
				t.Fatal(err)
			}
			if err := restored.QueryRow(ctx, query).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			if want != actual {
				t.Fatalf("complete base plus acknowledged delta differs for %s", table.Name)
			}
		}
	}
}

func TestLedgerHistoryArchiveOnlineAcknowledgesAllLedgerChangesAndRestoresFullSource(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	seedRetiredHistoryFixture(t, source)
	m.WithLedgerHistoryArchive(true)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	var complete bool
	var copied, staged, retired int64
	var mode string
	if err := target.QueryRow(ctx, "SELECT complete,copied,(SELECT ledger_history_mode FROM v3_migration_online.run) FROM v3_migration_online.progress WHERE name='ledger_entries'").Scan(&complete, &copied, &mode); err != nil || !complete || copied != 0 || mode != "archive" {
		t.Fatalf("ledger baseline=%t/%d mode=%s err=%v", complete, copied, mode, err)
	}
	recoverFullSource := ledgerArchivePrepareRestore(t, source, opts.RunID)
	other := NewImporter(source, target, m.crypto)
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"prepare_preview", func() error { _, err := other.PrepareOnline(ctx, opts, false); return err }},
		{"prepare_resume", func() error { _, err := other.PrepareOnline(ctx, opts, true); return err }},
		{"copy", func() error { _, err := other.CopyOnline(ctx, opts); return err }},
		{"sync", func() error { _, err := other.SyncOnline(ctx, opts); return err }},
		{"verify", func() error { _, err := other.VerifyOnline(ctx, opts); return err }},
	} {
		t.Run("refuse_copy_"+operation.name, func(t *testing.T) {
			if err := operation.run(); err == nil || !strings.Contains(err.Error(), "mode changed") {
				t.Fatalf("mode mismatch accepted: %v", err)
			}
		})
	}
	if _, err := source.Exec(ctx, `UPDATE billing.ledger_entries SET amount=25 WHERE entry_id='credit-original';
	 DELETE FROM billing.ledger_entries WHERE entry_id IN ('debit-original','retired-points-entry');
	 UPDATE billing.ledger_entries SET amount=9223372036854775806 WHERE entry_id='retired-gpt-entry';
	 INSERT INTO billing.ledger_entries SELECT 'late-live',account_id,reference_type,reference_id,entry_type,direction,amount,balance_after,'late-live-key',reason_code,reason_detail,operator_type,operator_id,metadata,created_at FROM billing.ledger_entries WHERE entry_id='credit-original';
	 INSERT INTO billing.ledger_entries SELECT 'late-retired',account_id,reference_type,reference_id,entry_type,direction,amount,balance_after,'late-retired-key',reason_code,reason_detail,operator_type,operator_id,metadata,created_at FROM billing.ledger_entries WHERE entry_id='retired-gpt-entry'`); err != nil {
		t.Fatal(err)
	}
	onlineMigrationSync(t, m, opts)
	var acknowledged, pending int64
	if err := source.QueryRow(ctx, "SELECT count(*) FILTER(WHERE acked),count(*) FILTER(WHERE NOT acked) FROM v3_migration_capture.events WHERE table_name='billing.ledger_entries'").Scan(&acknowledged, &pending); err != nil || acknowledged != 6 || pending != 0 {
		t.Fatalf("ledger capture ACK=%d pending=%d err=%v", acknowledged, pending, err)
	}
	if err := target.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+onlineStage("v3_billing.historical_entries")+"),(SELECT count(*) FROM v3_migration_online.retired_rows WHERE name='ledger_entries')").Scan(&staged, &retired); err != nil || staged != 0 || retired != 0 {
		t.Fatalf("ledger projection or retirement leaked=%d/%d err=%v", staged, retired, err)
	}
	var delta bytes.Buffer
	if _, err := ExportOnlineDelta(ctx, source, opts.RunID, &delta); err != nil {
		t.Fatal(err)
	}
	var ledgerKeys, ledgerRows int
	capturedTables := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(delta.Bytes()), []byte{'\n'}) {
		var rec onlineDeltaRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "header" {
			for _, table := range rec.Tables {
				capturedTables[table.Name] = true
			}
		}
		if rec.Table == "billing.ledger_entries" && rec.Kind == "delete" {
			ledgerKeys++
		}
		if rec.Table == "billing.ledger_entries" && rec.Kind == "row" {
			ledgerRows++
		}
	}
	if ledgerKeys != 6 || ledgerRows != 4 {
		t.Fatalf("ACK events omitted from complete delta: ledger keys=%d rows=%d", ledgerKeys, ledgerRows)
	}
	for _, table := range []string{"billing.ledger_entries", "billing.accounts", "billing.funding_lots", "billing.funding_allocations", "migration_source.logs", "migration_source.passkey_credentials", "gateway.request_audits", "gateway.request_attempt_audits"} {
		if !capturedTables[table] {
			t.Fatalf("archive projection clipped complete capture table %s", table)
		}
	}
	if report, err := m.VerifyOnline(ctx, opts); err != nil || report.Phase != "verified" || report.LedgerHistoryMode != "archive" {
		t.Fatalf("archive verify=%+v err=%v", report, err)
	}
	if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatal(err)
	}
	if report, err := other.FinalizeOnline(ctx, opts); err == nil || report.Applied || !strings.Contains(err.Error(), "mode changed") {
		t.Fatalf("copy finalize accepted archive run: %+v err=%v", report, err)
	}
	if report, err := m.FinalizeOnline(ctx, opts); err != nil || !report.Applied || report.LedgerHistoryArchive == nil || report.LedgerHistoryArchive.EntryCount != 4 {
		t.Fatalf("archive finalize=%+v err=%v", report, err)
	}
	if report, err := m.Check(ctx); err != nil || len(report.Issues) > 0 {
		t.Fatalf("independent final check=%+v err=%v", report, err)
	}
	t.Run("real_full_backup_recovery", recoverFullSource)
}

func TestLedgerHistoryArchiveOnlineRetiredReceiptGuardsRemainExact(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	seedRetiredHistoryFixture(t, source)
	m.WithLedgerHistoryArchive(true)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	for _, tc := range []struct{ name, key, value string }{
		{"unexpected_zero", "retired.history.fabricated", "0"},
		{"nonzero_ledger", "retired.history.retired:ledger_entries", "1"},
		{"nonzero_ledger_sum", "retired.history.retired:ledger_entries.amount_v2_units", "18446744073709551614"},
		{"funding_sum", "retired_features.billing_funding_lots.remaining_amount_v2_units", "9223372036854775808"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before string
			err := target.QueryRow(ctx, "SELECT value::text FROM v3_migration_online.totals WHERE name=$1", tc.key).Scan(&before)
			if err != nil && err != pgx.ErrNoRows {
				t.Fatal(err)
			}
			existed := err == nil
			mutate := func(remove bool, value string) {
				t.Helper()
				if err := pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
					if err := onlineAuthorize(ctx, tx, opts.RunID); err != nil {
						return err
					}
					if remove {
						_, err := tx.Exec(ctx, "DELETE FROM v3_migration_online.totals WHERE name=$1", tc.key)
						return err
					}
					_, err := tx.Exec(ctx, "INSERT INTO v3_migration_online.totals(name,value)VALUES($1,$2::numeric)ON CONFLICT(name)DO UPDATE SET value=EXCLUDED.value", tc.key, value)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			mutate(false, tc.value)
			if report, err := m.VerifyOnline(ctx, opts); err == nil || report.Applied || !strings.Contains(err.Error(), "retired receipt") {
				t.Fatalf("archive bypassed retired receipt guard: %+v err=%v", report, err)
			}
			mutate(!existed, before)
		})
	}
	if report, err := m.VerifyOnline(ctx, opts); err != nil || report.Phase != "verified" {
		t.Fatalf("restored exact receipts refused: %+v err=%v", report, err)
	}
}

func TestLedgerHistoryArchiveOnlineCopyRunRefusesArchiveMode(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	if report, err := m.PrepareOnline(ctx, opts, true); err != nil || !report.Applied || report.LedgerHistoryMode != "copy" {
		t.Fatalf("default copy prepare=%+v err=%v", report, err)
	}
	other := NewImporter(source, target, m.crypto).WithLedgerHistoryArchive(true)
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"prepare_preview", func() error { _, err := other.PrepareOnline(ctx, opts, false); return err }},
		{"prepare_resume", func() error { _, err := other.PrepareOnline(ctx, opts, true); return err }},
		{"copy", func() error { _, err := other.CopyOnline(ctx, opts); return err }},
		{"sync", func() error { _, err := other.SyncOnline(ctx, opts); return err }},
		{"verify", func() error { _, err := other.VerifyOnline(ctx, opts); return err }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); err == nil || !strings.Contains(err.Error(), "mode changed") {
				t.Fatalf("archive mode accepted copy run: %v", err)
			}
		})
	}
	if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatal(err)
	}
	if report, err := other.FinalizeOnline(ctx, opts); err == nil || report.Applied || !strings.Contains(err.Error(), "mode changed") {
		t.Fatalf("archive finalize accepted copy run: %+v err=%v", report, err)
	}
}
