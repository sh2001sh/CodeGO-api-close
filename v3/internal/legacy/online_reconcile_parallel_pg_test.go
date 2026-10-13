//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Every write, lock and malformed row below belongs to disposable fixtures.
func retainedParallelPool(t *testing.T, original *pgxpool.Pool, max int32) *pgxpool.Pool {
	t.Helper()
	cfg := original.Config()
	cfg.MaxConns = max
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func retainedParallelFixture(t *testing.T) (*Importer, *pgxpool.Pool, *pgxpool.Pool, OnlineOptions) {
	t.Helper()
	m, source, target, opts := onlineMigrationFixture(t, true)
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.users(id,username,role,status,"group",quota,claude_quota,setting) VALUES(8,'consumer',1,1,'default',0,0,'{}')`)
	seedChannelMarketFixture(t, source)
	for _, table := range channelMarketSourceTables {
		if !cmStreamedTable(table) {
			continue
		}
		keys := "id"
		if table == "pelican_artifacts" {
			keys = "group_id,model"
		}
		onlineRecoveryExec(t, source, "ALTER TABLE "+pgx.Identifier{"marketplace", table}.Sanitize()+" ADD PRIMARY KEY("+keys+")")
	}
	// Use exactly four migration connections; observer/lock-holder pools remain
	// separate so the proof does not accidentally starve a domain of a slot.
	m.source = retainedParallelPool(t, source, 4)
	m.pool = retainedParallelPool(t, target, 4)
	return m, source, target, opts
}

func retainedParallelPrepare(t *testing.T, m *Importer, source, target *pgxpool.Pool, opts OnlineOptions, gap func()) OnlineOptions {
	t.Helper()
	retainedCopiedFixture(t, m, opts)
	capture := retainedPauseFixture(t, source, opts)
	if gap != nil {
		gap()
	}
	next := retainedNewEpochFixture(t, source, target, opts, capture)
	if _, err := m.PrepareRetainedOnline(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	return next.OnlineOptions
}

func retainedParallelAssertReleased(t *testing.T, m *Importer, target *pgxpool.Pool) {
	t.Helper()
	if m.source.Stat().AcquiredConns() != 0 || m.pool.Stat().AcquiredConns() != 0 {
		t.Fatalf("worker connections leaked: source=%d target=%d", m.source.Stat().AcquiredConns(), m.pool.Stat().AcquiredConns())
	}
	var locks int
	if err := target.QueryRow(context.Background(), `SELECT count(*) FROM pg_locks WHERE locktype='advisory'
	 AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid=0 AND objid=$1::oid AND objsubid=1`, onlineLock).Scan(&locks); err != nil || locks != 0 {
		t.Fatalf("coordinator lock leaked: count=%d err=%v", locks, err)
	}
}

func TestOnlineRetainedParallelDomainsSnapshotCaptureAndExactAmounts(t *testing.T) {
	m, source, target, opts := retainedParallelFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	opts = retainedParallelPrepare(t, m, source, target, opts, func() {
		onlineRecoveryExec(t, source, `UPDATE billing.funding_lots SET remaining_amount=55 WHERE lot_id='funding-box-lot';
		 UPDATE billing.funding_allocations SET amount=45 WHERE allocation_id='funding-box-allocation';
		 UPDATE billing.funding_lots SET remaining_amount=9223372036854775806 WHERE lot_id='funding-retired-lot';
		 UPDATE marketplace.ranking_snapshots SET request_count=102 WHERE id='ranking-201'`)
	})
	block, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = block.Rollback(context.Background()) }()
	if _, err := block.Exec(ctx, "SELECT name FROM v3_migration_online.progress WHERE name='request_audits' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	type result struct {
		report OnlineReconcileReport
		err    error
	}
	done := make(chan result, 1)
	go func() { r, err := m.ReconcileRetainedOnline(ctx, opts); done <- result{r, err} }()
	// Funding and market must actually commit while request is blocked. A
	// goroutine count alone cannot detect singleton-row serialization.
	deadline := time.Now().Add(20 * time.Second)
	for {
		var independent bool
		err = target.QueryRow(ctx, `SELECT bool_and(complete) FROM v3_migration_online.progress
		 WHERE name IN ('funding_lots','funding_allocations') OR name LIKE 'market.%'`).Scan(&independent)
		if err != nil {
			t.Fatal(err)
		}
		if independent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("funding and market did not commit while request domain was blocked")
		}
		select {
		case r := <-done:
			t.Fatalf("reconcile ended before blocked domain: %+v %v", r.report, r.err)
		case <-time.After(25 * time.Millisecond):
		}
	}
	if m.source.Stat().AcquiredConns() > 4 || m.pool.Stat().AcquiredConns() > 4 {
		t.Fatal("four-connection budget exceeded")
	}
	// All workers inherit the exported snapshot. This write must be invisible
	// to the delayed request worker and remain captured for subsequent Sync.
	onlineRecoveryExec(t, source, "UPDATE gateway.request_audits SET quota=4503599627370497 WHERE request_id='kept-request'")
	if err := block.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil || !r.report.Applied || r.report.Phase != "copied" {
		t.Fatalf("parallel %+v %v", r.report, r.err)
	}
	var amount int64
	if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.request_audits")+" WHERE request_id='kept-request'").Scan(&amount); err != nil || amount != 100 {
		t.Fatalf("worker used a newer snapshot: %d %v", amount, err)
	}
	var remaining int64
	if err := target.QueryRow(ctx, "SELECT remaining_amount FROM "+onlineStage("v3_billing.funding_lots")+" WHERE lot_id='funding-box-lot'").Scan(&remaining); err != nil || remaining != 110 {
		t.Fatalf("funding amount=%d err=%v", remaining, err)
	}
	var pending, acked int
	if err := source.QueryRow(ctx, "SELECT count(*) FILTER(WHERE NOT acked),count(*) FILTER(WHERE acked) FROM v3_migration_capture.events").Scan(&pending, &acked); err != nil || pending != 1 || acked != 0 {
		t.Fatalf("reconcile consumed capture pending=%d acked=%d err=%v", pending, acked, err)
	}
	retainedParallelAssertReleased(t, m, target)
	onlineMigrationSync(t, m, opts)
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.request_audits")+" WHERE request_id='kept-request'").Scan(&amount); err != nil || amount != 9007199254740994 {
		t.Fatalf("large integer capture lost precision: %d %v", amount, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("funding and market committed despite a blocked request domain; shared snapshot, unacked capture, exact large amounts and independent Verify passed")
}

func TestOnlineRetainedParallelLateWorkerFailureRollbackResume(t *testing.T) {
	m, source, target, opts := retainedParallelFixture(t)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `INSERT INTO gateway.request_audits
	 SELECT (jsonb_populate_record(NULL::gateway.request_audits,to_jsonb(s)||jsonb_build_object('request_id','a'||lpad(g::text,5,'0'),'quota',1))).*
	 FROM gateway.request_audits s CROSS JOIN generate_series(1,8192)g WHERE request_id='kept-request'`)
	opts = retainedParallelPrepare(t, m, source, target, opts, func() {
		onlineRecoveryExec(t, source, `UPDATE gateway.request_audits SET quota=4503599627370497 WHERE request_id='a00001';
		 UPDATE gateway.request_audits SET quota=55 WHERE request_id='a04100';
		 UPDATE gateway.request_audits SET quota=-1 WHERE request_id='a04200'`)
	})
	r, err := m.ReconcileRetainedOnline(ctx, opts)
	if err == nil || r.Applied {
		t.Fatalf("bad second page accepted: %+v %v", r, err)
	}
	var raw []byte
	if err := target.QueryRow(ctx, "SELECT cursor FROM v3_migration_online.progress WHERE name='request_audits'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var cursor onlineReconcileCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.Scanned != 4096 || cursor.Repaired != 1 {
		t.Fatalf("committed checkpoint=%s err=%v", raw, err)
	}
	var amount, committed int64
	if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.request_audits")+" WHERE request_id='a04100'").Scan(&amount); err != nil || amount != 2 {
		t.Fatalf("failed page leaked amount=%d err=%v", amount, err)
	}
	if err := target.QueryRow(ctx, "SELECT COALESCE(sum((cursor->>'scanned')::bigint),0) FROM v3_migration_online.progress WHERE name NOT LIKE 'recovery_previous:%'").Scan(&committed); err != nil || committed != r.Scanned {
		t.Fatalf("report counts uncommitted work: reported=%d committed=%d err=%v", r.Scanned, committed, err)
	}
	retainedParallelAssertReleased(t, m, target)
	if _, err := m.VerifyOnline(ctx, opts); err == nil {
		t.Fatal("incomplete parallel run verified")
	}
	onlineRecoveryAssertNoMoney(t, target)
	onlineRecoveryExec(t, source, "UPDATE gateway.request_audits SET quota=1 WHERE request_id='a04200'; UPDATE gateway.request_audits SET quota=4503599627370498 WHERE request_id='a00001'")
	if r, err := m.ReconcileRetainedOnline(ctx, opts); err != nil || !r.Applied {
		t.Fatalf("same run resume %+v %v", r, err)
	}
	onlineMigrationSync(t, m, opts)
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.request_audits")+" WHERE request_id='a00001'").Scan(&amount); err != nil || amount != 9007199254740996 {
		t.Fatalf("below-cursor capture lost: amount=%d err=%v", amount, err)
	}
	retainedParallelAssertReleased(t, m, target)
	t.Log("first 4096 request rows committed, invalid second page rolled back, workers released, same-run resume plus captured below-cursor change independently verified")
}

func TestOnlineRetainedParallelWorkerAuthorityAndOperationLock(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	opts = retainedParallelPrepare(t, m, source, target, opts, nil)
	coordinator, err := m.onlineConnection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOnlineConnection(coordinator)
	pid := coordinator.Conn().PgConn().PID()
	check := func(run string, owner uint32, allowed bool) {
		t.Helper()
		tx, err := target.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		err = onlineAuthorizeReconcileWorker(ctx, tx, run, owner)
		if (err == nil) != allowed {
			t.Fatalf("authority run=%s owner=%d allowed=%v err=%v", run, owner, allowed, err)
		}
		if allowed {
			var writer string
			if err := tx.QueryRow(ctx, "SELECT current_setting('codego.online_writer',true)").Scan(&writer); err != nil || writer != run {
				t.Fatalf("writer gate=%s err=%v", writer, err)
			}
		}
	}
	check(opts.RunID, pid, true)
	check("wrong-run", pid, false)
	check(opts.RunID, pid+1000000, false)
	otherDB, err := source.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherDB.Exec(ctx, "SELECT pg_advisory_lock($1)", onlineLock); err != nil {
		t.Fatal(err)
	}
	check(opts.RunID, otherDB.Conn().PgConn().PID(), false)
	if _, err := otherDB.Exec(ctx, "SELECT pg_advisory_unlock($1)", onlineLock); err != nil {
		t.Fatal(err)
	}
	otherDB.Release()
	if _, err := m.SyncOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "operation") {
		t.Fatalf("second operation bypassed coordinator: %v", err)
	}
	setPhase := func(phase string) {
		t.Helper()
		tx, err := coordinator.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := onlineAuthorize(ctx, tx, opts.RunID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "UPDATE v3_migration_online.run SET phase=$1 WHERE singleton", phase); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	setPhase("copied")
	check(opts.RunID, pid, false)
	setPhase("reconciling")
	if _, err := coordinator.Exec(ctx, "SELECT pg_advisory_unlock($1)", onlineLock); err != nil {
		t.Fatal(err)
	}
	check(opts.RunID, pid, false)
	if _, err := coordinator.Exec(ctx, "SELECT pg_advisory_lock(0,$1::int)", onlineLock); err != nil {
		t.Fatal(err)
	}
	check(opts.RunID, pid, false)
	if _, err := coordinator.Exec(ctx, "SELECT pg_advisory_unlock(0,$1::int)", onlineLock); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Exec(ctx, "SELECT pg_advisory_lock_shared($1)", onlineLock); err != nil {
		t.Fatal(err)
	}
	check(opts.RunID, pid, false)
	t.Log("worker rejects wrong run, PID, phase and lost lock; separate online operation rejected; writer gate requires coordinator authority")
}

func TestOnlineRetainedParallelInsufficientPoolsFailBeforeWork(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	for _, budget := range []struct{ source, target int32 }{{3, 4}, {4, 3}, {2, 2}} {
		t.Run(fmt.Sprintf("source%d_target%d", budget.source, budget.target), func(t *testing.T) {
			candidate := *m
			candidate.source = retainedParallelPool(t, source, budget.source)
			candidate.pool = retainedParallelPool(t, target, budget.target)
			r, err := candidate.ReconcileRetainedOnline(context.Background(), opts)
			if err == nil || !strings.Contains(err.Error(), "four") || r.Scanned != 0 || r.Applied {
				t.Fatalf("insufficient pool %+v %v", r, err)
			}
			retainedParallelAssertReleased(t, &candidate, target)
		})
	}
}

func TestOnlineRetainedParallelWideDomainRows(t *testing.T) {
	m, source, target, opts := retainedParallelFixture(t)
	ctx := context.Background()
	const width = 60 << 20
	opts = retainedParallelPrepare(t, m, source, target, opts, func() {
		for _, sql := range []string{
			"UPDATE gateway.request_audits SET error_code=repeat('r',$1) WHERE request_id='kept-request'",
			"UPDATE billing.funding_lots SET reference_id=repeat('f',$1) WHERE lot_id='funding-box-lot'",
			"UPDATE marketplace.verification_runs SET summary=repeat('m',$1) WHERE id='verify-201'",
		} {
			if _, err := source.Exec(ctx, sql, width); err != nil {
				t.Fatal(err)
			}
		}
	})
	if r, err := m.ReconcileRetainedOnline(ctx, opts); err != nil || !r.Applied {
		t.Fatalf("wide parallel rows %+v %v", r, err)
	}
	for _, sql := range []string{
		"SELECT length(error_code) FROM " + onlineStage("v3_audit.request_audits") + " WHERE request_id='kept-request'",
		"SELECT length(reference_id) FROM " + onlineStage("v3_billing.funding_lots") + " WHERE lot_id='funding-box-lot'",
		"SELECT length(summary) FROM " + onlineStage("v3_channelmarket.verification_runs") + " WHERE id='verify-201'",
	} {
		var length int
		if err := target.QueryRow(ctx, sql).Scan(&length); err != nil || length != width {
			t.Fatalf("wide field length=%d err=%v", length, err)
		}
	}
	retainedParallelAssertReleased(t, m, target)
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("three 60MiB domain rows reconciled with exact fields and all connections released under the external memory budget")
}
