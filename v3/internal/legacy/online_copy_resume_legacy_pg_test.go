//go:build pgintegration

package legacy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestOnlineCopyResumesLegacyPageWithDeferredLogNormalization(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs
 SELECT 10000+g,7,1700010000+g,2,'resume legacy','alice','key','chat-model',1,1,1,0,false,13,11,'default','','resume-'||g,'','{}'
 FROM generate_series(1,8192)g;
 UPDATE migration_source.logs SET created_at=1700030000,request_id='resume-page-repeat' WHERE id IN(14092,14093)`)
	if _, err := m.PrepareOnline(ctx, opts, true); err != nil {
		t.Fatal(err)
	}
	read, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	write, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = write.Rollback(ctx) }()
	if err := onlineAuthorize(ctx, write, opts.RunID); err != nil {
		t.Fatal(err)
	}
	sources, specs, err := onlineBindings(ctx, read, write, opts.RunID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := loadOnlineProjector(ctx, read, write, sources)
	if err != nil {
		t.Fatal(err)
	}
	if err := onlineRefreshAccounts(ctx, write, p); err != nil {
		t.Fatal(err)
	}
	var logs onlineSpec
	for _, spec := range specs {
		if spec.name == "logs" {
			logs = spec
		}
	}
	inputs, err := onlineReadBatch(ctx, read, logs, nil)
	if err != nil || len(inputs) != 4096 {
		t.Fatalf("legacy first page rows=%d err=%v", len(inputs), err)
	}
	// The previous release replaced rows and normalized every page. Leave its
	// committed checkpoint intact and switch to the optimized CopyOnline.
	if err := p.apply(ctx, logs, inputs); err != nil {
		t.Fatal(err)
	}
	if _, err := write.Exec(ctx, "UPDATE v3_migration_online.progress SET cursor=$1,copied=4096 WHERE name='logs'", inputs[len(inputs)-1].key); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := read.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryStageUsage(t, target, 14092, "resume-page-repeat", 1700030000)
	// A mutation below the committed cursor must remain captured for sync;
	// restarting Copy must neither replay that page nor discard the change.
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET quota=55 WHERE id=10001")
	if r, err := m.CopyOnline(ctx, opts); err != nil || r.Phase != "copied" || r.Tables["logs"] != 4100 {
		t.Fatalf("resume %+v err=%v", r, err)
	}
	for _, id := range []int64{14092, 14093} {
		var actual, expected string
		if err := target.QueryRow(ctx, "SELECT request_id FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id=$1", id).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if err := source.QueryRow(ctx, "SELECT request_id||':v2-log:'||id FROM migration_source.logs WHERE id=$1", id).Scan(&expected); err != nil || actual != expected {
			t.Fatalf("mixed-release duplicate id=%d actual=%q expected=%q err=%v", id, actual, expected, err)
		}
	}
	var count, copied, amount int64
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.events")).Scan(&count); err != nil || count != 8196 {
		t.Fatalf("resumed rows=%d err=%v", count, err)
	}
	if err := target.QueryRow(ctx, "SELECT copied FROM v3_migration_online.progress WHERE name='logs'").Scan(&copied); err != nil || copied != 8196 {
		t.Fatalf("resumed checkpoint=%d err=%v", copied, err)
	}
	if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.events")+" WHERE id=10001").Scan(&amount); err != nil || amount != 2 {
		t.Fatalf("committed page was recopied amount=%d err=%v", amount, err)
	}
	onlineMigrationSync(t, m, opts)
	if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.events")+" WHERE id=10001").Scan(&amount); err != nil || amount != 110 {
		t.Fatalf("below-cursor mutation lost amount=%d err=%v", amount, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("legacy checkpoint retained; cross-page duplicate IDs, exact totals and below-cursor capture replay preserved")
}
