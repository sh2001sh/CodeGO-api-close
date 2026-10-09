//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func onlineMigrationFixture(t *testing.T, funding bool) (*Importer, *pgxpool.Pool, *pgxpool.Pool, OnlineOptions) {
	t.Helper()
	source, target, crypto := importTestDB(t)
	historyFixture(t, source)
	if funding {
		seedFundingFixture(t, source)
	} else {
		seedRetiredHistoryFixture(t, source)
	}
	opts := OnlineOptions{RunID: "online-pipeline-fixture-20261009", SourceAdmin: source}
	return NewImporter(source, target, crypto), source, target, opts
}
func onlineMigrationSync(t *testing.T, m *Importer, opts OnlineOptions) {
	t.Helper()
	for i := 0; i < 100; i++ {
		r, err := m.SyncOnline(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if r.Pending == 0 {
			return
		}
	}
	t.Fatal("online fixture did not catch up")
}
func onlineMigrationReady(t *testing.T, m *Importer, opts OnlineOptions) {
	t.Helper()
	ctx := context.Background()
	if r, err := m.PrepareOnline(ctx, opts, true); err != nil || !r.Applied {
		t.Fatalf("prepare %+v %v", r, err)
	}
	if r, err := m.CopyOnline(ctx, opts); err != nil || r.Phase != "copied" {
		t.Fatalf("copy %+v %v", r, err)
	}
	onlineMigrationSync(t, m, opts)
	if r, err := m.VerifyOnline(ctx, opts); err != nil || r.Phase != "verified" {
		t.Fatalf("verify %+v %v", r, err)
	}
}

func TestOnlineMigrationHistoryAndFundingCatchUpAtomicOpening(t *testing.T) {
	for _, funding := range []bool{false, true} {
		t.Run(map[bool]string{false: "retired_history", true: "funding"}[funding], func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, funding)
			ctx := context.Background()
			if r, err := m.PrepareOnline(ctx, opts, false); err != nil || r.Applied {
				t.Fatalf("preview %+v %v", r, err)
			}
			var captures bool
			if err := source.QueryRow(ctx, "SELECT to_regclass('v3_migration_capture.events') IS NOT NULL").Scan(&captures); err != nil || captures {
				t.Fatalf("preview wrote source: %v %v", captures, err)
			}
			onlineMigrationReady(t, m, opts)
			var money int
			if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.ledger_entries").Scan(&money); err != nil || money != 0 {
				t.Fatalf("pre-import posted money %d %v", money, err)
			}
			if _, err := target.Exec(ctx, "UPDATE "+onlineStage("v3_audit.events")+" SET amount=0 WHERE id=51"); err == nil {
				t.Fatal("unowned staging write accepted")
			}
			if _, err := m.Import(ctx, true); err == nil {
				t.Fatal("offline import silently bypassed the staged protocol")
			}
			if _, err := m.FinalizeOnline(ctx, opts); err == nil {
				t.Fatal("finalized a live unsealed source")
			}
			_, err := source.Exec(ctx, `DELETE FROM migration_source.logs WHERE id=54;
		 UPDATE migration_source.logs SET quota=55,content='changed after baseline' WHERE id=51;
		 UPDATE billing.ledger_entries SET amount=25,reason_detail='changed ledger' WHERE entry_id='credit-original';
		 INSERT INTO gateway.request_attempt_audits SELECT 'late-orphan','missing-parent',attempt_no,retry_index,channel_id,model_name,fault_domain,request_type,status,success,status_code,failure_class,stage,started_at,completed_at,duration_ms,created_at FROM gateway.request_attempt_audits WHERE attempt_id='kept-attempt'`)
			if err != nil {
				t.Fatal(err)
			}
			if funding {
				if _, err = source.Exec(ctx, "UPDATE billing.funding_lots SET remaining_amount=55 WHERE lot_id='funding-box-lot'; UPDATE billing.funding_allocations SET amount=45 WHERE allocation_id='funding-box-allocation'"); err != nil {
					t.Fatal(err)
				}
			}
			onlineMigrationSync(t, m, opts)
			// Parent-only changes must move an existing attempt out of the orphan
			// archive without any UPDATE of the attempt itself.
			if _, err = source.Exec(ctx, `INSERT INTO gateway.request_audits SELECT 'missing-parent',trace_id,user_id,token_id,model_name,group_name,protocol,request_type,status,counted_in_success_rate,billable,quota,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at,created_at,updated_at FROM gateway.request_audits WHERE request_id='kept-request'`); err != nil {
				t.Fatal(err)
			}
			onlineMigrationSync(t, m, opts)
			if _, err = SealOnlineCapture(ctx, source, opts.RunID); err != nil {
				t.Fatal(err)
			}
			if report, err := m.FinalizeOnline(ctx, opts); err != nil || !report.Applied || len(report.Issues) > 0 {
				t.Fatalf("final report %+v error %v", report, err)
			}
			if report, err := m.Check(ctx); err != nil || len(report.Issues) > 0 {
				t.Fatalf("independent full offline check %+v %v", report, err)
			}
			var balance, entries int64
			if err = target.QueryRow(ctx, "SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'").Scan(&balance); err != nil || balance != 1000 {
				t.Fatalf("wallet %d %v", balance, err)
			}
			if err = target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='opening'").Scan(&entries); err != nil || entries != 1 {
				t.Fatalf("opening %d %v", entries, err)
			}
			if _, err = m.FinalizeOnline(ctx, opts); err == nil {
				t.Fatal("completed migration could be finalized twice")
			}
			var after int64
			if err = target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='opening'").Scan(&after); err != nil || after != entries {
				t.Fatal("replay changed money")
			}
			var request string
			if err = target.QueryRow(ctx, "SELECT request_id FROM v3_billing.usage_logs WHERE id=51").Scan(&request); err != nil || request != "kept-request" {
				t.Fatalf("duplicate-to-singleton normalization %q %v", request, err)
			}
			var linked bool
			if err = target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM v3_audit.request_attempt_audits WHERE attempt_id='late-orphan')").Scan(&linked); err != nil || !linked {
				t.Fatal("parent insertion did not reclassify orphan")
			}
			var kind string
			if err = target.QueryRow(ctx, "SELECT relkind::text FROM pg_class WHERE oid='v3_billing.usage_logs'::regclass").Scan(&kind); err != nil || kind != "p" {
				t.Fatal("usage partitioning lost")
			}
			var invalidFK int
			if err = target.QueryRow(ctx, "SELECT count(*) FROM pg_constraint WHERE contype='f' AND NOT convalidated AND connamespace IN ('v3_billing'::regnamespace,'v3_audit'::regnamespace)").Scan(&invalidFK); err != nil || invalidFK != 0 {
				t.Fatal("final foreign keys unvalidated")
			}
			t.Logf("online baseline, updates/deletion, parent-only reclassification, full independent check, exactly one opening, and usage partitioning passed")
		})
	}
}

func TestOnlineMigrationBadDeltaRollbackAndDependencyRefusal(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	var before []byte
	if err := target.QueryRow(ctx, "SELECT to_jsonb(t) FROM "+onlineStage("v3_audit.events")+" t WHERE id=51").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, "UPDATE migration_source.logs SET quota=-1 WHERE id=51"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SyncOnline(ctx, opts); err == nil {
		t.Fatal("invalid usage delta accepted")
	}
	var after []byte
	if err := target.QueryRow(ctx, "SELECT to_jsonb(t) FROM "+onlineStage("v3_audit.events")+" t WHERE id=51").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed delta left partial staging update")
	}
	var pending bool
	if err := source.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM v3_migration_capture.events WHERE NOT acked)").Scan(&pending); err != nil || !pending {
		t.Fatal("failed delta acknowledged source")
	}
	if _, err := source.Exec(ctx, "UPDATE migration_source.logs SET quota=50 WHERE id=51"); err != nil {
		t.Fatal(err)
	}
	onlineMigrationSync(t, m, opts)
	if _, err := source.Exec(ctx, "UPDATE billing.accounts SET account_type='point' WHERE account_id='wallet-7'"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SyncOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "structural dependency") {
		t.Fatalf("remapping dependency was not rejected: %v", err)
	}
	var native int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_identity.users").Scan(&native); err != nil || native != 0 {
		t.Fatal("failed online work exposed native users")
	}
}

func TestOnlineMigrationFinalFailureRollsBackAdoptionAndMoney(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	// A small final-domain collision is detected only after table adoption and
	// wallet opening have run. Both must roll back together.
	if _, err := target.Exec(ctx, "INSERT INTO v3_identity.users(id,username,email,role,status,group_name,settings)VALUES(7,'different-person','different@example.invalid','user','active','default','{}')"); err != nil {
		t.Fatal(err)
	}
	if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.FinalizeOnline(ctx, opts); err == nil {
		t.Fatal("final identity conflict accepted")
	}
	var money int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.ledger_entries").Scan(&money); err != nil || money != 0 {
		t.Fatalf("failed final posted money %d %v", money, err)
	}
	var historical int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.historical_entries").Scan(&historical); err != nil || historical != 0 {
		t.Fatal("failed final adopted tables")
	}
	var phase string
	if err := target.QueryRow(ctx, "SELECT phase FROM v3_migration_online.run").Scan(&phase); err != nil || phase != "verified" {
		t.Fatal("failed final invalidated baseline")
	}
	if _, err := source.Exec(ctx, "UPDATE migration_source.users SET username='changed' WHERE id=7"); err == nil {
		t.Fatal("failed final silently released source fence")
	}
	if _, err := UnsealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, "UPDATE migration_source.users SET username='changed' WHERE id=7"); err != nil {
		t.Fatal("explicit source rollback did not reopen V2 writes")
	}
}
