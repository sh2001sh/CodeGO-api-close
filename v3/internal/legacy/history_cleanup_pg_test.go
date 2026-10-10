//go:build pgintegration

package legacy

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func TestHistoryCleanupAtomicFinanceProtectionAndPartitionReclamation(t *testing.T) {
	m, _, target, _ := onlineMigrationFixture(t, false)
	cutoff := time.Unix(1700000001, 0).UTC()
	m.WithHistoryCutoff(cutoff)
	ctx := context.Background()
	if _, err := m.Import(ctx, true); err != nil {
		t.Fatal(err)
	}
	cleanupAt := cutoff.Add(24 * time.Hour)
	var account int64
	if err := target.QueryRow(ctx, "SELECT id FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'").Scan(&account); err != nil {
		t.Fatal(err)
	}
	_, err := target.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,key_id,amount,request_id,model,terminal) VALUES
	 ($1,$3,7,11,12,'expired-usage','chat-model','completed'),($1,$3,7,11,14,'open-finance','chat-model','completed'),($2,$3,7,11,16,'fresh-usage','chat-model','completed');
	 INSERT INTO v3_billing.reservations(account_id,amount,expires_at,request_id) VALUES($3,14,$2::timestamptz+interval '1 day','open-finance')`, pgx.QueryExecModeSimpleProtocol, cutoff, cleanupAt, account)
	if err != nil {
		t.Fatal(err)
	}
	control, err := identity.NewControl(target, identity.ControlConfig{SessionSecret: make([]byte, 32), EncryptionKey: make([]byte, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := control.ListKeys(ctx, 7, 0, 10)
	if err != nil || len(keys) != 1 || keys[0].SpentMicroCredits != 162 {
		t.Fatalf("spent before=%+v %v", keys, err)
	}
	_, err = target.Exec(ctx, `CREATE FUNCTION public.reject_test_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'intentional cleanup failure'; END $$;
	 CREATE TRIGGER reject_test_cleanup BEFORE DELETE ON v3_audit.events FOR EACH ROW EXECUTE FUNCTION public.reject_test_cleanup()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audit.CleanupHistoriesBefore(ctx, target, cleanupAt, 5000); err == nil {
		t.Fatal("injected failure was swallowed")
	}
	var amount int64
	if err := target.QueryRow(ctx, "SELECT amount FROM v3_billing.retired_usage_totals WHERE user_id=7 AND key_id=11").Scan(&amount); err != nil || amount != 120 {
		t.Fatalf("rollback aggregate=%d %v", amount, err)
	}
	if _, err := target.Exec(ctx, "DROP TRIGGER reject_test_cleanup ON v3_audit.events"); err != nil {
		t.Fatal(err)
	}
	if _, err := audit.CleanupHistoriesBefore(ctx, target, cleanupAt, 5000); err != nil {
		t.Fatal(err)
	}
	keys, err = control.ListKeys(ctx, 7, 0, 10)
	if err != nil || keys[0].SpentMicroCredits != 162 {
		t.Fatalf("spent after=%+v %v", keys, err)
	}
	var rows int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.usage_logs WHERE request_id IN('open-finance','fresh-usage')").Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("protected/boundary=%d %v", rows, err)
	}
	if err := target.QueryRow(ctx, "SELECT balance FROM v3_billing.accounts WHERE id=$1", account).Scan(&amount); err != nil || amount != 1000 {
		t.Fatalf("wallet changed=%d %v", amount, err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_audit.events WHERE id=52").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("financial refund evidence=%d %v", rows, err)
	}
	if err := ledger.EnsureUsagePartitions(ctx, target, time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := audit.ReclaimExpiredUsagePartitions(ctx, target, cleanupAt); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := target.QueryRow(ctx, "SELECT to_regclass('v3_billing.usage_logs_202301') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("expired partition=%v %v", exists, err)
	}
	if _, err := audit.CleanupHistoriesBefore(ctx, target, cleanupAt, 0); err == nil {
		t.Fatal("invalid batch accepted")
	}
}

func TestHistoryCleanupUnknownRecentOrphanAndBoundedFamily(t *testing.T) {
	m, _, target, _ := onlineMigrationFixture(t, false)
	cutoff := time.Unix(1700000001, 0).UTC()
	m.WithHistoryCutoff(cutoff)
	ctx := context.Background()
	if _, err := m.Import(ctx, true); err != nil {
		t.Fatal(err)
	}
	cleanupAt := cutoff.Add(24 * time.Hour)
	_, err := target.Exec(ctx, `INSERT INTO v3_audit.request_audits
	 SELECT (jsonb_populate_record(NULL::v3_audit.request_audits,to_jsonb(r)||jsonb_build_object('request_id',x.id,'created_at',$1::timestamptz,'started_at',$1::timestamptz,'completed_at',CASE WHEN x.id='unknown-parent' THEN '0001-01-01'::timestamptz ELSE $1::timestamptz END,'status',CASE WHEN x.id='unknown-parent' THEN 'historical_unknown' ELSE 'succeeded' END))).* FROM v3_audit.request_audits r CROSS JOIN (VALUES('unknown-parent'),('large-family')) x(id) WHERE r.request_id='kept-request';
	 INSERT INTO v3_audit.request_attempt_audits
	 SELECT (jsonb_populate_record(NULL::v3_audit.request_attempt_audits,to_jsonb(a)||jsonb_build_object('attempt_id','large-child-'||i,'request_id','large-family','created_at',$1::timestamptz,'started_at',$1::timestamptz,'completed_at',$1::timestamptz))).* FROM v3_audit.request_attempt_audits a CROSS JOIN generate_series(1,7)i WHERE a.attempt_id='kept-attempt';
	 INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,key_id,amount,request_id,model,terminal)
	 SELECT $1,id,7,11,18,'unknown-parent','chat-model','completed' FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet';
	 INSERT INTO v3_audit.request_samples(request_id,user_id,model,created_at,request_body,response_body) VALUES('unknown-parent',7,'chat-model',$1,'{}','{}'),('expired-sample',7,'chat-model',$1,'{}','{}')`, pgx.QueryExecModeSimpleProtocol, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	_, err = target.Exec(ctx, `INSERT INTO v3_audit.orphan_request_attempt_history(attempt_id,request_id,source_record)
	 SELECT 'recent-start-orphan','missing-parent',to_jsonb(a)||jsonb_build_object('attempt_id','recent-start-orphan','request_id','missing-parent','created_at',$1::timestamptz,'completed_at',$1::timestamptz,'started_at',$2::timestamptz) FROM v3_audit.request_attempt_audits a WHERE attempt_id='kept-attempt'`, cutoff, cleanupAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audit.CleanupHistoriesBefore(ctx, target, cleanupAt, 2); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_audit.request_attempt_audits").Scan(&count); err != nil || count != 6 {
		t.Fatalf("bounded child batch remaining=%d %v", count, err)
	}
	for i := 0; i < 4; i++ {
		if _, err := audit.CleanupHistoriesBefore(ctx, target, cleanupAt, 2); err != nil {
			t.Fatal(err)
		}
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_audit.request_audits WHERE request_id='large-family'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("empty family parent=%d %v", count, err)
	}
	for _, query := range []string{
		"SELECT count(*) FROM v3_audit.request_audits WHERE request_id='unknown-parent'",
		"SELECT count(*) FROM v3_billing.usage_logs WHERE request_id='unknown-parent'",
		"SELECT count(*) FROM v3_audit.orphan_request_attempt_history WHERE attempt_id='recent-start-orphan'",
	} {
		if err := target.QueryRow(ctx, query).Scan(&count); err != nil || count != 1 {
			t.Fatalf("protected boundary %s: %d %v", query, count, err)
		}
	}
	if n, err := audit.New(target, audit.Config{}).DeleteSamplesBefore(ctx, cleanupAt); err != nil || n != 1 {
		t.Fatalf("sample cleanup=%d %v", n, err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_audit.request_samples WHERE request_id='unknown-parent'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("unknown sample=%d %v", count, err)
	}
}
