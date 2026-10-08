//go:build pgintegration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/desktop"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func TestProductionRequestSummariesRefreshRealDesktopHealthAndCache(t *testing.T) {
	t.Setenv("V3_FILES_DIR", t.TempDir())
	t.Setenv("V3_AUDIT_SAMPLE_RATE_PPM", "0")
	f := newContractFixture(t)
	ctx := context.Background()
	first, _ := f.request(contractBody(false, "warm"), "127.0.0.1:1234")
	second, rounds := f.request(contractBody(false, "retry"), "127.0.0.1:1234")
	if first.Code != 200 || second.Code != 200 {
		t.Fatalf("production requests failed: %d %d", first.Code, second.Code)
	}
	if len(rounds.postgres) != 0 {
		t.Fatalf("metadata producer added synchronous PostgreSQL: %v", rounds.postgres)
	}
	waitRequestSummaries(t, f, 2)
	var attempts, retries, channel, amount int64
	if err := f.deps.PG.QueryRow(ctx, `SELECT attempts_count,retry_count,final_channel_id,amount FROM v3_audit.request_audits WHERE request_id=$1`, second.Header().Get("X-Request-Id")).Scan(&attempts, &retries, &channel, &amount); err != nil || attempts != 2 || retries != 1 || channel != 2 || amount != 20 {
		t.Fatalf("retry did not preserve logical row and exact native charge: attempts=%d retries=%d channel=%d amount=%d %v", attempts, retries, channel, amount, err)
	}

	// Execute an actual failing upstream without creating a billing row. Health
	// must count the observed failure rather than infer it from charged usage.
	s := audit.NewRequestRecorder(ctx, f.deps.PG.Pool, sampleTestLog(io.Discard))
	h := summaryHTTPGateway(t, s, "error_before/t0")
	summaryHTTPRequest(h, httptest.NewRecorder(), true)
	s.Close()
	waitRequestSummaries(t, f, 3)
	var billedFailures int
	if err := f.deps.PG.QueryRow(ctx, `SELECT count(*) FROM v3_billing.usage_logs l JOIN v3_audit.request_audits a USING(request_id) WHERE a.status='failed'`).Scan(&billedFailures); err != nil || billedFailures != 0 {
		t.Fatalf("failed request incorrectly required charged usage: %d %v", billedFailures, err)
	}
	if err := f.deps.Redis.XGroupCreateMkStream(ctx, redisx.StreamBillingEvents, redisx.GroupLedger, "0").Err(); err != nil {
		t.Fatal(err)
	}
	worker, err := ledger.NewWorker(f.deps.PG.Pool, f.deps.Redis, ledger.WorkerConfig{Consumer: "request-summary-fixture", Block: time.Millisecond}, sampleTestLog(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := worker.Step(ctx); err != nil || count == 0 {
		t.Fatalf("real settlement worker failed: %d %v", count, err)
	}

	id, err := identity.NewControl(f.deps.PG.Pool, identity.ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := desktop.New(f.deps.PG.Pool, id, desktop.Config{Crypto: f.deps.Crypto, PublicURL: "http://localhost"})
	auth, err := d.Start(ctx, desktop.StartInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Decide(ctx, 1, auth.SessionID, true); err != nil {
		t.Fatal(err)
	}
	grant, err := d.Poll(ctx, auth.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/desktop/group-status", nil)
	r.Header.Set("Authorization", "Bearer "+grant.AccessToken)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, r)
	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			Group       string   `json:"group"`
			Count       int64    `json:"request_count"`
			SuccessRate *float64 `json:"success_rate"`
			CacheRate   *float64 `json:"cache_hit_rate"`
			Models      []struct {
				Series []json.RawMessage `json:"series"`
			} `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || w.Code != 200 || !payload.Success || len(payload.Data) != 1 {
		t.Fatalf("real desktop status refused: %d %s %v", w.Code, w.Body.String(), err)
	}
	g := payload.Data[0]
	if g.Group != "default" || g.Count != 3 || g.SuccessRate == nil || *g.SuccessRate != float64(2)/3*100 || g.CacheRate == nil || *g.CacheRate != 0 || len(g.Models[0].Series) != 12 {
		t.Fatalf("fresh runtime health/cache facts missing: %+v", g)
	}
	var samples int
	if err := f.deps.PG.QueryRow(ctx, `SELECT count(*) FROM v3_audit.request_samples`).Scan(&samples); err != nil || samples != 0 {
		t.Fatalf("always-on summary enabled sensitive sampling: %d %v", samples, err)
	}
	t.Log("actual production HTTP success/retry + failing upstream -> 3 request summaries, 66.666% health, native settled usage cache=0%, 12 real buckets; synchronous PostgreSQL=0; body sampling disabled")
}

func waitRequestSummaries(t *testing.T, f *contractFixture, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int
		if err := f.deps.PG.QueryRow(context.Background(), `SELECT count(*) FROM v3_audit.request_audits`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("background summaries count=%d want=%d", count, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
