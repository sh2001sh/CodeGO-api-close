//go:build pgintegration

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/bench/mockupstream"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

// This uses the production handler, real routing/admission/settlement and an
// isolated PostgreSQL/Redis fixture. All upstream traffic stays on localhost.
func TestProduction500ConcurrentMockStreamsAndExactLedger(t *testing.T) {
	f := newContractFixture(t)
	ctx := context.Background()
	const concurrency = 500
	var active, peak, reached atomic.Int64
	var armed atomic.Bool
	gate := make(chan struct{})
	var release sync.Once
	mock := mockupstream.Handler()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		if armed.Load() {
			if reached.Add(1) == concurrency {
				release.Do(func() { close(gate) })
			}
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		mock.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(func() { release.Do(func() { close(gate) }) })
	for _, query := range []string{
		`UPDATE v3_identity.users SET max_concurrency=750,requests_per_minute=0 WHERE id=1`,
		`UPDATE v3_catalog.channels SET max_concurrency=750,max_user_concurrency=750`,
		`UPDATE v3_catalog.channel_credentials SET max_concurrency=750`,
	} {
		if _, err := f.deps.PG.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.deps.PG.Exec(ctx, `UPDATE v3_catalog.channels SET base_url=$1`, upstream.URL+"/m/complete/c4/i10/t20"); err != nil {
		t.Fatal(err)
	}
	if err := catalog.NewPublisher(f.deps.PG.Pool, f.deps.Redis, f.deps.Crypto, f.deps.Crypto, runtimeLogger()).PublishNow(ctx); err != nil {
		t.Fatal(err)
	}
	background := context.WithValue(ctx, contractContextKey{}, contractBackground)
	handler, closeFn, err := prodHandler(background, f.deps, runtimeLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFn)
	f.handler = handler
	warmNativeFixture(t, f)
	before := f.balances(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	transport := &http.Transport{MaxIdleConns: 1024, MaxIdleConnsPerHost: 750, MaxConnsPerHost: 750}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	first, elapsed := make([]time.Duration, concurrency), make([]time.Duration, concurrency)
	errs := make(chan error, concurrency)
	var wg sync.WaitGroup
	start := make(chan struct{})
	armed.Store(true)
	timer := time.AfterFunc(20*time.Second, func() { release.Do(func() { close(gate) }) })
	defer timer.Stop()
	for i := range concurrency {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			// Ramp socket creation while retaining all 500 active streams at
			// the gate; this avoids measuring Windows SYN-backlog limits.
			time.Sleep(time.Duration(i/10) * 5 * time.Millisecond)
			began := time.Now()
			req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(contractBody(true, "load500")))
			if err != nil {
				errs <- err
				return
			}
			req.Header.Set("Authorization", "Bearer "+f.key)
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				errs <- fmt.Errorf("status=%d body=%s", resp.StatusCode, body)
				return
			}
			reader, done, chunks := sse.NewReader(resp.Body, 0), false, 0
			for {
				e, err := reader.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					errs <- err
					return
				}
				text := string(e.Data)
				if strings.Contains(text, `"content":"tok`) {
					chunks++
					if first[i] == 0 {
						first[i] = time.Since(began)
					}
				}
				done = done || text == "[DONE]"
			}
			elapsed[i] = time.Since(began)
			if !done || chunks != 4 {
				errs <- fmt.Errorf("incomplete stream: done=%v chunks=%d", done, chunks)
			}
		}(i)
	}
	began := time.Now()
	close(start)
	wg.Wait()
	wall := time.Since(began)
	close(errs)
	failures := 0
	for err := range errs {
		failures++
		if failures <= 3 {
			t.Error(err)
		}
	}
	if failures > 0 {
		t.Fatalf("failed_requests=%d upstream_peak=%d reached=%d", failures, peak.Load(), reached.Load())
		return
	}
	if peak.Load() < concurrency {
		t.Fatalf("only %d concurrent upstream streams admitted, want %d", peak.Load(), concurrency)
	}
	assertContractDebit(t, before, f.balances(t), concurrency*20)
	f.assertNoHolds(t)
	// A rejected request after the burst must not charge or emit another event.
	if w := nativeRequest(f, "POST", "/v1/chat/completions", `{"model":"forbidden","messages":[{"role":"user","content":"load-boundary"}]}`, "application/json", true); w.Code != http.StatusForbidden {
		t.Fatalf("forbidden model status=%d", w.Code)
	}
	assertContractDebit(t, before, f.balances(t), concurrency*20)
	if err := f.deps.Redis.XGroupCreateMkStream(ctx, redisx.StreamBillingEvents, redisx.GroupLedger, "0").Err(); err != nil {
		t.Fatal(err)
	}
	worker, err := ledger.NewWorker(f.deps.PG.Pool, f.deps.Redis, ledger.WorkerConfig{Consumer: "load500", Batch: 64, Block: time.Millisecond}, runtimeLogger())
	if err != nil {
		t.Fatal(err)
	}
	settleStart := time.Now()
	for {
		n, err := worker.Step(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	var logs, debits, total, wallet, budget int64
	if err := f.deps.PG.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM v3_billing.usage_logs),
	 (SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='usage'),
	 (SELECT coalesce(sum(amount),0)::bigint FROM v3_billing.usage_logs),
	 (SELECT balance FROM v3_billing.accounts WHERE id=$1),
	 (SELECT balance FROM v3_billing.accounts WHERE id=$2)`, f.wallet, f.budget).Scan(&logs, &debits, &total, &wallet, &budget); err != nil {
		t.Fatal(err)
	}
	const allRequests = concurrency + 1 // fixture warmup
	if logs != allRequests || debits != allRequests*2 || total != allRequests*20 || wallet != 1_000_000_000-total || budget != wallet {
		t.Fatalf("logs=%d debits=%d charged=%d wallet=%d budget=%d", logs, debits, total, wallet, budget)
	}
	percentile := func(data []time.Duration, p int) time.Duration {
		ordered := slices.Clone(data)
		slices.Sort(ordered)
		return ordered[(len(ordered)-1)*p/100]
	}
	t.Logf("concurrency=%d upstream_peak=%d requests_per_second=%.1f wall=%v TTFT_p50=%v TTFT_p95=%v TTFT_p99=%v total_p50=%v total_p99=%v ledger_drain=%v logs=%d debits=%d charged_micro=%d",
		concurrency, peak.Load(), concurrency/wall.Seconds(), wall, percentile(first, 50), percentile(first, 95), percentile(first, 99), percentile(elapsed, 50), percentile(elapsed, 99), time.Since(settleStart), logs, debits, total)
}
