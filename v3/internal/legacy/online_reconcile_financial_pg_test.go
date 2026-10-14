//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"
)

func TestOnlineRetainedFinancialReconciliationKeepsDiagnosticsAndRepairsExactMoney(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	ctx := context.Background()
	retainedCopiedFixture(t, m, opts)
	unchanged := retainedRowXmin(t, target, 53)
	financialVerificationStageChange(t, target, opts, "UPDATE "+onlineStage("v3_billing.usage_logs")+" SET key_id=999 WHERE id=51")
	capture := retainedPauseFixture(t, source, opts)
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET content=repeat('diagnostic',400000) WHERE id=53;
	 UPDATE migration_source.logs SET quota=9007199254740993,content='complete changed log',other='{"cached_tokens":123}' WHERE id=51;
	 DELETE FROM migration_source.logs WHERE id=54;
	 INSERT INTO migration_source.logs SELECT 55,user_id,created_at+1,type,'inserted log',username,token_name,model_name,7,prompt_tokens,completion_tokens,use_time,is_stream,channel_id,token_id,"group",ip,'',upstream_request_id,other FROM migration_source.logs WHERE id=51`)
	next := retainedNewEpochFixture(t, source, target, opts, capture)
	next.FinancialHistoryVerification = true
	if _, err := m.PrepareRetainedOnline(ctx, next); err != nil {
		t.Fatal(err)
	}
	r, err := m.ReconcileRetainedOnline(ctx, next.OnlineOptions)
	if err != nil || !r.Applied || r.HistoryVerification != "financial" || r.Pruned < 1 {
		t.Fatalf("financial repair=%+v err=%v", r, err)
	}
	if retainedRowXmin(t, target, 53) != unchanged {
		t.Fatal("diagnostic-only difference rewrote retained row")
	}
	var amount, content, metadata string
	var key int64
	if err := target.QueryRow(ctx, "SELECT amount::text,key_id FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id=51").Scan(&amount, &key); err != nil || amount != "18014398509481986" || key == 999 {
		t.Fatalf("money=%s key=%d err=%v", amount, key, err)
	}
	if err := target.QueryRow(ctx, "SELECT content,metadata::text FROM "+onlineStage("v3_audit.events")+" WHERE id=51").Scan(&content, &metadata); err != nil || content != "complete changed log" || !strings.Contains(metadata, "123") {
		t.Fatalf("complete source reread missing: content=%s metadata=%s err=%v", content, metadata, err)
	}
	onlineMigrationSync(t, m, next.OnlineOptions)
	if _, err := m.VerifyOnline(ctx, next.OnlineOptions); err != nil {
		t.Fatal(err)
	}
	full := next.OnlineOptions
	full.FinancialHistoryVerification = false
	if _, err := m.VerifyOnline(ctx, full); err == nil {
		t.Fatal("default full verification ignored diagnostic differences")
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineRetainedFinancialReconciliationFullRereadFailureRollsBackPage(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	ctx := context.Background()
	retainedCopiedFixture(t, m, opts)
	before := retainedRowXmin(t, target, 51)
	capture := retainedPauseFixture(t, source, opts)
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET quota=quota+1 WHERE id=51;
	 UPDATE migration_source.logs SET quota=quota+1,other='invalid diagnostic JSON' WHERE id=53`)
	next := retainedNewEpochFixture(t, source, target, opts, capture)
	next.FinancialHistoryVerification = true
	if _, err := m.PrepareRetainedOnline(ctx, next); err != nil {
		t.Fatal(err)
	}
	r, err := m.ReconcileRetainedOnline(ctx, next.OnlineOptions)
	if err == nil || r.Applied || !strings.Contains(err.Error(), "metadata is invalid JSON") {
		t.Fatalf("bad full row accepted=%+v err=%v", r, err)
	}
	if retainedRowXmin(t, target, 51) != before {
		t.Fatal("failed page committed its earlier money repair")
	}
	var copied int64
	if err := target.QueryRow(ctx, "SELECT copied FROM v3_migration_online.progress WHERE name='logs'").Scan(&copied); err != nil || copied != 0 {
		t.Fatalf("failed financial cursor advanced=%d err=%v", copied, err)
	}
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET other='{}' WHERE id=53`)
	if _, err := m.ReconcileRetainedOnline(ctx, next.OnlineOptions); err != nil {
		t.Fatal(err)
	}
	onlineMigrationSync(t, m, next.OnlineOptions)
	if _, err := m.VerifyOnline(ctx, next.OnlineOptions); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}
