//go:build pgintegration

package audit

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRequestRecordBatchPreservesReplayPerformanceAndAtomicFailures(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := New(pool, Config{})
	now := time.Now().UTC()
	base := RequestRecord{Model: "model", Group: "actual-group", Protocol: "openai_chat", RequestType: "stream", Status: "success", Counted: true,
		UserID: 1, KeyID: 2, ChannelID: 3, Attempts: 2, Retries: 1, PromptTokens: 10, CompletionTokens: 5, StartedAt: now, CompletedAt: now.Add(time.Second)}
	records := make([]RequestRecord, 128)
	for i := range records {
		records[i] = base
		records[i].RequestID = fmt.Sprintf("batch-%d", i)
		records[i].Amount = int64(i)
	}
	first, generation := 1.25, 200.0
	records[0].TTFTMS, records[0].GenerationMS = &first, &generation
	for range 2 {
		if err := s.RecordRequests(ctx, records); err != nil {
			t.Fatal(err)
		}
	}
	var count, amount, attempts, retries int64
	if err := pool.QueryRow(ctx, `SELECT count(*),sum(amount),max(attempts_count),max(retry_count) FROM v3_audit.request_audits`).Scan(&count, &amount, &attempts, &retries); err != nil || count != 128 || amount != 8128 || attempts != 2 || retries != 1 {
		t.Fatalf("batch/replay lost observations: count=%d amount=%d attempts=%d retries=%d err=%v", count, amount, attempts, retries, err)
	}
	var ttft, window *float64
	if err := pool.QueryRow(ctx, `SELECT ttft_ms,generation_ms FROM v3_audit.request_audits WHERE request_id='batch-0'`).Scan(&ttft, &window); err != nil || ttft == nil || *ttft != first || window == nil || *window != generation {
		t.Fatalf("batch lost real timing: %v %v %v", ttft, window, err)
	}
	if err := pool.QueryRow(ctx, `SELECT ttft_ms,generation_ms FROM v3_audit.request_audits WHERE request_id='batch-1'`).Scan(&ttft, &window); err != nil || ttft != nil || window != nil {
		t.Fatalf("batch fabricated timing: %v %v %v", ttft, window, err)
	}
	duplicate := base
	duplicate.RequestID = "duplicate-in-batch"
	later := duplicate
	later.Amount, later.CompletedAt = 123, now.Add(2*time.Second)
	if err := s.RecordRequests(ctx, []RequestRecord{duplicate, later, duplicate}); err != nil {
		t.Fatal("duplicate logical ID in same batch was not idempotent", err)
	}
	if err := pool.QueryRow(ctx, `SELECT amount FROM v3_audit.request_audits WHERE request_id=$1`, duplicate.RequestID).Scan(&amount); err != nil || amount != 123 {
		t.Fatal("duplicate replay lost maximum settled amount", amount, err)
	}
	valid, collision := base, records[0]
	valid.RequestID, collision.UserID = "collision-sibling", 99
	if err := s.RecordRequests(ctx, []RequestRecord{valid, collision}); err == nil {
		t.Fatal("batch request identity collision was silently accepted")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_audit.request_audits WHERE request_id='collision-sibling'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("conflicting batch partially committed", count, err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION v3_audit.reject_batch_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced test storage failure'; END $$;
 CREATE TRIGGER reject_batch_test BEFORE INSERT ON v3_audit.request_audits FOR EACH ROW WHEN (NEW.request_id='forced-db-error') EXECUTE FUNCTION v3_audit.reject_batch_test()`); err != nil {
		t.Fatal(err)
	}
	valid.RequestID, collision = "database-error-sibling", base
	collision.RequestID = "forced-db-error"
	if err := s.RecordRequests(ctx, []RequestRecord{valid, collision}); err == nil {
		t.Fatal("database error was swallowed")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_audit.request_audits WHERE request_id IN ('database-error-sibling','forced-db-error')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("database error batch partially committed", count, err)
	}
	valid.RequestID = "after-database-error"
	if err := s.RecordRequest(ctx, valid); err != nil {
		t.Fatal("failed transaction poisoned subsequent writes", err)
	}
}
