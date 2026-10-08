//go:build pgintegration

package audit

import (
	"context"
	"testing"
	"time"
)

func TestRequestPerformancePersistencePreservesUnknownAndRejectsFakeSamples(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := New(pool, Config{})
	now := time.Now().UTC()
	r := RequestRecord{RequestID: "legacy-observation", Model: "model", Group: "market-group", Protocol: "openai_chat", RequestType: "stream",
		Status: "success", Counted: true, UserID: 1, KeyID: 2, ChannelID: 3, Attempts: 1,
		PromptTokens: 10, CompletionTokens: 12, StartedAt: now, CompletedAt: now.Add(time.Second)}
	if err := s.RecordRequest(ctx, r); err != nil {
		t.Fatal(err)
	}
	var ttft, generation *float64
	var tokens int64
	if err := pool.QueryRow(ctx, `SELECT ttft_ms,generation_ms,completion_tokens FROM v3_audit.request_audits WHERE request_id=$1`, r.RequestID).Scan(&ttft, &generation, &tokens); err != nil || ttft != nil || generation != nil || tokens != 12 {
		t.Fatalf("historical/unknown observation fabricated timing or changed accounting: %v %v %d %v", ttft, generation, tokens, err)
	}
	r.RequestID = "observed-performance"
	first, window := 1.2, 300.0
	r.TTFTMS, r.GenerationMS = &first, &window
	for range 2 {
		if err := s.RecordRequest(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	var throughput float64
	if err := pool.QueryRow(ctx, `SELECT ttft_ms,generation_ms,completion_tokens/(generation_ms/1000) FROM v3_audit.request_audits WHERE request_id=$1`, r.RequestID).Scan(&ttft, &generation, &throughput); err != nil || ttft == nil || *ttft != first || generation == nil || *generation != window || throughput != 40 {
		t.Fatalf("real performance sample not persisted precisely: %v %v %v %v", ttft, generation, throughput, err)
	}
	changed := 999.0
	r.TTFTMS = &changed
	if err := s.RecordRequest(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT ttft_ms FROM v3_audit.request_audits WHERE request_id=$1`, r.RequestID).Scan(&ttft); err != nil || ttft == nil || *ttft != first {
		t.Fatalf("replay replaced first observed timing: %v %v", ttft, err)
	}
	for _, assignment := range []string{
		`ttft_ms=0`, `ttft_ms=-1`, `ttft_ms='NaN'::float8`, `ttft_ms='Infinity'::float8`,
		`generation_ms=0`, `generation_ms=-1`, `generation_ms='NaN'::float8`, `completion_tokens=0`, `request_type='sync'`, `status='cancelled'`,
	} {
		if _, err := pool.Exec(ctx, `UPDATE v3_audit.request_audits SET `+assignment+` WHERE request_id=$1`, r.RequestID); err == nil {
			t.Fatalf("database accepted impossible performance sample: %s", assignment)
		}
	}
}
