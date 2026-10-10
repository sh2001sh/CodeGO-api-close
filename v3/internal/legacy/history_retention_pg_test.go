//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func retentionHistoryFixture(t *testing.T, source *pgxpool.Pool, cutoff time.Time) {
	t.Helper()
	_, err := source.Exec(context.Background(), `INSERT INTO gateway.request_audits
	 SELECT (jsonb_populate_record(NULL::gateway.request_audits,to_jsonb(r)||jsonb_build_object('request_id','old-parent','created_at',$1::timestamptz-interval '2 days','started_at',$1::timestamptz-interval '2 days','completed_at',$1::timestamptz-interval '2 days'))).* FROM gateway.request_audits r WHERE request_id='kept-request';
	 INSERT INTO gateway.request_attempt_audits SELECT (jsonb_populate_record(NULL::gateway.request_attempt_audits,to_jsonb(a)||jsonb_build_object('attempt_id','old-child','request_id','old-parent','created_at',$1::timestamptz-interval '2 days','started_at',$1::timestamptz-interval '2 days','completed_at',$1::timestamptz-interval '2 days'))).* FROM gateway.request_attempt_audits a WHERE attempt_id='kept-attempt'`, pgx.QueryExecModeSimpleProtocol, cutoff)
	if err != nil {
		t.Fatal(err)
	}
}

func insertRetentionChild(t *testing.T, source *pgxpool.Pool, id, parent string, at time.Time) {
	t.Helper()
	_, err := source.Exec(context.Background(), `INSERT INTO gateway.request_attempt_audits SELECT (jsonb_populate_record(NULL::gateway.request_attempt_audits,to_jsonb(a)||jsonb_build_object('attempt_id',$1::text,'request_id',$2::text,'created_at',$3::timestamptz,'started_at',$3::timestamptz,'completed_at',$3::timestamptz))).* FROM gateway.request_attempt_audits a WHERE attempt_id='kept-attempt'`, id, parent, at)
	if err != nil {
		t.Fatal(err)
	}
}

func TestHistoryRetentionOfflineBoundaryFinanceAndLifetimeUsage(t *testing.T) {
	m, source, target, _ := onlineMigrationFixture(t, false)
	cutoff := time.Unix(1700000001, 0).UTC()
	m.WithHistoryCutoff(cutoff)
	retentionHistoryFixture(t, source, cutoff)
	insertRetentionChild(t, source, "recent-child", "old-parent", cutoff)
	insertRetentionChild(t, source, "recent-orphan", "missing-parent", cutoff)
	insertRetentionChild(t, source, "expired-orphan", "missing-parent", cutoff.Add(-48*time.Hour))
	r, err := m.Import(context.Background(), true)
	if err != nil || len(r.Issues) > 0 {
		t.Fatalf("import=%+v %v", r, err)
	}
	var n int64
	for _, query := range []string{"SELECT count(*) FROM v3_audit.events WHERE id=52", "SELECT count(*) FROM v3_audit.request_audits WHERE request_id='old-parent'", "SELECT count(*) FROM v3_audit.orphan_request_attempt_history WHERE attempt_id='recent-orphan'"} {
		if err := target.QueryRow(context.Background(), query).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s: %d %v", query, n, err)
		}
	}
	if err := target.QueryRow(context.Background(), "SELECT count(*) FROM v3_billing.usage_logs").Scan(&n); err != nil || n != 0 {
		t.Fatalf("expired usage=%d %v", n, err)
	}
	if err := target.QueryRow(context.Background(), "SELECT amount FROM v3_billing.retired_usage_totals WHERE user_id=7 AND key_id=11").Scan(&n); err != nil || n != 120 {
		t.Fatalf("lifetime usage=%d %v", n, err)
	}
	if err := target.QueryRow(context.Background(), "SELECT count(*) FROM v3_audit.request_attempt_audits WHERE request_id='old-parent'").Scan(&n); err != nil || n != 2 {
		t.Fatalf("complete attempt family=%d %v", n, err)
	}
	if err := target.QueryRow(context.Background(), "SELECT count(*) FROM v3_audit.orphan_request_attempt_history WHERE attempt_id='expired-orphan'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("expired orphan=%d %v", n, err)
	}
	if _, err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.WithHistoryCutoff(cutoff.Add(time.Second)).Check(context.Background()); err == nil {
		t.Fatal("changed boundary accepted")
	}
	if _, err := target.Exec(context.Background(), "UPDATE v3_audit.history_retention SET cutoff=cutoff+interval '1 second'"); err == nil {
		t.Fatal("ordinary writer changed immutable cutoff")
	}
	m.WithHistoryCutoff(cutoff)
	for _, bad := range []struct {
		quota   int64
		message string
	}{{-1, "invalid retired Key usage"}, {9223372036854775807, "overflows micro-credits"}} {
		if _, err := source.Exec(context.Background(), "UPDATE migration_source.logs SET quota=$1 WHERE id=51", bad.quota); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Check(context.Background()); err == nil || !strings.Contains(err.Error(), bad.message) {
			t.Fatalf("invalid retired usage not rejected: %v", err)
		}
		if err := target.QueryRow(context.Background(), "SELECT amount FROM v3_billing.retired_usage_totals WHERE user_id=7 AND key_id=11").Scan(&n); err != nil || n != 120 {
			t.Fatalf("failed validation changed lifetime usage=%d %v", n, err)
		}
	}
}

func TestHistoryRetentionOnlineLateChildAndMovedParent(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	cutoff := time.Unix(1700000001, 0).UTC()
	m.WithHistoryCutoff(cutoff)
	retentionHistoryFixture(t, source, cutoff)
	onlineMigrationReady(t, m, opts)
	ctx := context.Background()
	var n int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.request_audits")+" WHERE request_id='old-parent'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("old parent=%d %v", n, err)
	}
	insertRetentionChild(t, source, "late-child", "old-parent", cutoff)
	onlineMigrationSync(t, m, opts)
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.request_audits")+" WHERE request_id='old-parent'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("late parent=%d %v", n, err)
	}
	// PostgreSQL capture JSON and Go-derived parent keys use different spacing.
	// A direct parent update and a child update must still replay that parent once.
	if _, err := source.Exec(ctx, "UPDATE gateway.request_audits SET trace_id='changed-with-child' WHERE request_id='kept-request'; UPDATE gateway.request_attempt_audits SET request_id='kept-request' WHERE attempt_id='late-child'"); err != nil {
		t.Fatal(err)
	}
	onlineMigrationSync(t, m, opts)
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.request_audits")+" WHERE request_id='old-parent'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("expired parent=%d %v", n, err)
	}
	if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.FinalizeOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Check(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryRetentionAllHistoryRejectsUnexpectedLifetimeUsage(t *testing.T) {
	m, _, target, _ := onlineMigrationFixture(t, false)
	ctx := context.Background()
	if _, err := m.Import(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Exec(ctx, "INSERT INTO v3_billing.retired_usage_totals(user_id,key_id,amount) VALUES(7,11,99)"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Check(ctx); err == nil || !strings.Contains(err.Error(), "aggregate count differs") {
		t.Fatalf("unexpected lifetime usage accepted in unfiltered migration: %v", err)
	}
}
