//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"
)

func TestOnlineSync4096EventsKeepLaterWorkPending(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs
 SELECT 10000+g,7,1700100000+g,2,'sync batch','alice','key','chat-model',1,1,1,0,false,13,11,'default','','sync-batch-'||g,'','{}'
 FROM generate_series(1,4097)g`)
	fingerprint := func() string {
		t.Helper()
		var value string
		if err := source.QueryRow(ctx, "SELECT md5(string_agg(to_jsonb(t)::text,',' ORDER BY id)) FROM migration_source.logs t").Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := fingerprint()
	first, err := m.SyncOnline(ctx, opts)
	if err != nil || first.Acknowledged != 4096 || first.Pending != 1 || first.Tables["logs"] != 4096 {
		t.Fatalf("bounded first round report=%+v err=%v", first, err)
	}
	var rows, amount, acked, pending int64
	if err := target.QueryRow(ctx, "SELECT count(*),sum(amount) FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id>10000").Scan(&rows, &amount); err != nil || rows != 4096 || amount != 8192 {
		t.Fatalf("first projection rows=%d amount=%d err=%v", rows, amount, err)
	}
	if err := source.QueryRow(ctx, "SELECT count(*) FILTER(WHERE acked),count(*) FILTER(WHERE NOT acked) FROM v3_migration_capture.events").Scan(&acked, &pending); err != nil || acked != 4096 || pending != 1 {
		t.Fatalf("first receipt acked=%d pending=%d err=%v", acked, pending, err)
	}
	second, err := m.SyncOnline(ctx, opts)
	if err != nil || second.Acknowledged != 1 || second.Pending != 0 {
		t.Fatalf("remaining event report=%+v err=%v", second, err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*),sum(amount) FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id>10000").Scan(&rows, &amount); err != nil || rows != 4097 || amount != 8194 {
		t.Fatalf("final projection rows=%d amount=%d err=%v", rows, amount, err)
	}
	if before != fingerprint() {
		t.Fatal("sync changed source rows")
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineSync4096LaterFailureRollsBackEarlierProjectionBatches(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	before := onlineRecoveryTotals(t, target)
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs
 SELECT 150000+g,7,1700150000+g,2,'before bad','alice','key','chat-model',1,1,1,0,false,13,11,'default','','before-bad-'||g,'','{}'
 FROM generate_series(1,512)g;
 INSERT INTO migration_source.logs VALUES(200000,7,1700200000,2,'bad','alice','key','chat-model',-1,1,1,0,false,13,11,'default','','bad-batch','','{}')`)
	if _, err := m.SyncOnline(ctx, opts); err == nil {
		t.Fatal("invalid 513th projection was accepted")
	}
	var pending, acked, escaped int64
	if err := source.QueryRow(ctx, "SELECT count(*) FILTER(WHERE NOT acked),count(*) FILTER(WHERE acked) FROM v3_migration_capture.events").Scan(&pending, &acked); err != nil || pending != 513 || acked != 0 || before != onlineRecoveryTotals(t, target) {
		t.Fatalf("failed round receipts pending=%d acked=%d err=%v", pending, acked, err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.events")+" WHERE id>=150000").Scan(&escaped); err != nil || escaped != 0 {
		t.Fatalf("earlier 512-row projection escaped rollback rows=%d err=%v", escaped, err)
	}
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET quota=1 WHERE id=200000")
	resumed, err := m.SyncOnline(ctx, opts)
	if err != nil || resumed.Acknowledged != 514 || resumed.Pending != 0 || resumed.Tables["logs"] != 513 {
		t.Fatalf("retry did not acknowledge exact events report=%+v err=%v", resumed, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineSync4096PreservesEventKeyByteLimits(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET content=content||' key budget' WHERE id IN(51,52,53)")
	// Disposable journal padding keeps the real typed ID while exercising the
	// independent byte limit; native capture and its schema stay unchanged.
	onlineRecoveryExec(t, source, "UPDATE v3_migration_capture.events SET row_key=row_key||jsonb_build_object('padding',repeat('k',2100000)) WHERE NOT acked")
	for i := 0; i < 3; i++ {
		report, err := m.SyncOnline(ctx, opts)
		if err != nil || report.Acknowledged != 1 || report.Pending != int64(2-i) {
			t.Fatalf("4MiB key budget report=%+v err=%v", report, err)
		}
	}
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET content=content||' oversized key' WHERE id=51")
	onlineRecoveryExec(t, source, "UPDATE v3_migration_capture.events SET row_key=row_key||jsonb_build_object('padding',repeat('k',65*1024*1024)) WHERE NOT acked")
	before := onlineRecoveryTotals(t, target)
	if _, err := m.SyncOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "source key exceeds 64 MiB") {
		t.Fatalf("oversized key accepted err=%v", err)
	}
	var pending int
	if err := source.QueryRow(ctx, "SELECT count(*) FROM v3_migration_capture.events WHERE NOT acked").Scan(&pending); err != nil || pending != 1 || before != onlineRecoveryTotals(t, target) {
		t.Fatalf("oversized key changed receipts pending=%d err=%v", pending, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}
