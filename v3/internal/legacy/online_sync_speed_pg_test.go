//go:build pgintegration

package legacy

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestOnlineSyncPendingCountBoundedAndZeroExact(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET content=content||' pending status' WHERE id=51;
 INSERT INTO v3_migration_capture.events(table_name,row_key)
 SELECT e.table_name,e.row_key FROM (SELECT table_name,row_key FROM v3_migration_capture.events WHERE NOT acked ORDER BY id LIMIT 1)e
 CROSS JOIN generate_series(1,12290)g`)
	var acknowledged int64
	for i, want := range []struct {
		pending int64
		atLeast bool
		acked   int64
	}{{4096, true, 4096}, {4096, true, 4096}, {3, false, 4096}, {0, false, 3}} {
		report, err := m.SyncOnline(ctx, opts)
		if err != nil || report.Pending != want.pending || report.PendingAtLeast != want.atLeast || report.Acknowledged != want.acked {
			t.Fatalf("round %d report=%+v err=%v", i, report, err)
		}
		acknowledged += report.Acknowledged
	}
	var pending, acked int64
	if err := source.QueryRow(ctx, "SELECT count(*) FILTER(WHERE NOT acked),count(*) FILTER(WHERE acked) FROM v3_migration_capture.events").Scan(&pending, &acked); err != nil || pending != 0 || acked != 12291 || acknowledged != acked {
		t.Fatalf("exact journal pending=%d acknowledged=%d report_total=%d err=%v", pending, acked, acknowledged, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("bounded positive status is explicit; exact zero only after 12291 exact acknowledgements; duplicate replay and financial verification passed")
}

func TestOnlineSyncThroughputComparison(t *testing.T) {
	for _, limit := range []int{4096, 16384} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, false)
			ctx := context.Background()
			onlineMigrationReady(t, m, opts)
			opts.SyncEventLimit = limit
			onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs
 SELECT 500000+g,7,1700500000+g,2,'throughput','alice','key','chat-model',1,1,1,0,false,13,11,'default','','throughput-'||g,'','{}'
 FROM generate_series(1,16384)g`)
			started := time.Now()
			var total int64
			rounds := 0
			for {
				r, err := m.SyncOnline(ctx, opts)
				if err != nil {
					t.Fatal(err)
				}
				total += r.Acknowledged
				rounds++
				if r.Pending == 0 {
					break
				}
			}
			elapsed := time.Since(started)
			var rows, amount int64
			if err := target.QueryRow(ctx, "SELECT count(*),sum(amount) FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id>500000").Scan(&rows, &amount); err != nil || rows != 16384 || amount != 32768 || total != 16384 {
				t.Fatalf("throughput rows=%d amount=%d total=%d err=%v", rows, amount, total, err)
			}
			onlineRecoveryAssertNoMoney(t, target)
			t.Logf("sync-throughput limit=%d events=%d rounds=%d seconds=%.3f events_per_second=%.2f", limit, total, rounds, elapsed.Seconds(), float64(total)/elapsed.Seconds())
		})
	}
}

func TestOnlineSync16384EventsPreserveProjectionAndLaterEvent(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	opts.SyncEventLimit = 16384
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs
 SELECT 300000+g,7,1700300000+g,2,'larger sync','alice','key','chat-model',1,1,1,0,false,13,11,'default','','larger-sync-'||g,'','{}'
 FROM generate_series(1,1000)g;
 INSERT INTO v3_migration_capture.events(table_name,row_key)
 SELECT e.table_name,e.row_key FROM (SELECT table_name,row_key FROM v3_migration_capture.events WHERE NOT acked ORDER BY id LIMIT 1)e
 CROSS JOIN generate_series(1,15385)g`)
	first, err := m.SyncOnline(ctx, opts)
	if err != nil || first.Acknowledged != 16384 || first.Pending != 1 || first.PendingAtLeast || first.Tables["logs"] != 1000 {
		t.Fatalf("larger first batch report=%+v err=%v", first, err)
	}
	var rows, amount int64
	if err := target.QueryRow(ctx, "SELECT count(*),sum(amount) FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id>300000").Scan(&rows, &amount); err != nil || rows != 1000 || amount != 2000 {
		t.Fatalf("larger projection rows=%d amount=%d err=%v", rows, amount, err)
	}
	second, err := m.SyncOnline(ctx, opts)
	if err != nil || second.Acknowledged != 1 || second.Pending != 0 || second.PendingAtLeast || second.Tables["logs"] != 1 {
		t.Fatalf("later replay report=%+v err=%v", second, err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*),sum(amount) FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id>300000").Scan(&rows, &amount); err != nil || rows != 1000 || amount != 2000 {
		t.Fatalf("replay changed money rows=%d amount=%d err=%v", rows, amount, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("16384-event batch kept later event pending; 1000 deduplicated exact projections and 2000-unit money unchanged after replay")
}

func TestOnlineSyncInvalidEventLimitRejectedBeforeDatabase(t *testing.T) {
	for _, limit := range []int{-1, 16385} {
		if _, err := (&Importer{}).SyncOnline(context.Background(), OnlineOptions{SyncEventLimit: limit}); err == nil {
			t.Fatalf("accepted invalid event limit %d", limit)
		}
	}
}
