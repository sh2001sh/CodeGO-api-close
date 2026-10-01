//go:build pgintegration

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func TestProductionWarmRequestRoundTripContract(t *testing.T) {
	f := newContractFixture(t)
	// Warming is itself the real request pipeline, not injected auth, route or
	// settlement data. Cold cache misses and EVALSHA's NOSCRIPT recovery are
	// reported separately and do not count as the established warm budget.
	warm, cold := f.request(contractBody(false, "warm"), "127.0.0.1:3456")
	if warm.Code != http.StatusOK {
		t.Fatalf("cold request status=%d body=%s", warm.Code, warm.Body)
	}
	t.Logf("cold request: synchronous PG=%d Redis=%d detached=%d commands=%v", len(cold.postgres), len(cold.redis), cold.detached, cold.redis)
	if len(cold.postgres) == 0 {
		t.Fatal("cold fixture unexpectedly avoided real PostgreSQL identity/balance loading")
	}
	for _, streaming := range []bool{false, true} {
		name := "nonstream"
		if streaming {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			before, calls := f.balances(t), f.upstreamCalls.Load()
			response, count := f.request(contractBody(streaming, "hello"), "127.0.0.1:3456")
			if response.Code != 200 || !strings.Contains(response.Body.String(), "hello") {
				t.Fatalf("response status=%d body=%s", response.Code, response.Body)
			}
			if streaming && !strings.Contains(response.Body.String(), "[DONE]") {
				t.Fatal("stream lacks terminal marker")
			}
			if f.upstreamCalls.Load()-calls != 1 {
				t.Fatal("one-attempt fixture did not call exactly one upstream")
			}
			count.assertWarm(t, 4)
			if count.detached != 2 {
				t.Fatalf("counted detached Redis calls=%d want lease release + billing finalize = 2", count.detached)
			}
			assertContractDebit(t, before, f.balances(t), 20)
			f.assertNoHolds(t)
		})
	}
	t.Run("invalid_parameters", func(t *testing.T) {
		before, calls := f.balances(t), f.upstreamCalls.Load()
		response, count := f.request(`{"messages":[]}`, "127.0.0.1:3456")
		if response.Code != 400 {
			t.Fatalf("status=%d body=%s", response.Code, response.Body)
		}
		count.assertWarm(t, 0)
		if f.upstreamCalls.Load() != calls {
			t.Fatal("invalid parameters reached upstream")
		}
		assertContractDebit(t, before, f.balances(t), 0)
	})
	t.Run("user_concurrency_rejection", func(t *testing.T) {
		ctx := context.Background()
		key := redisx.KeyConcurrencyPrefix + "user:1"
		if err := f.deps.Redis.ZAdd(ctx, key, redis.Z{Score: float64(contractFutureScore()), Member: "fixture-held"}).Err(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := f.deps.Redis.ZRem(ctx, key, "fixture-held").Err(); err != nil {
				t.Error(err)
			}
		})
		before, calls := f.balances(t), f.upstreamCalls.Load()
		response, count := f.request(contractBody(false, "blocked"), "127.0.0.1:3456")
		if response.Code != 429 {
			t.Fatalf("status=%d body=%s", response.Code, response.Body)
		}
		count.assertWarm(t, 3)
		if count.detached != 1 {
			t.Fatalf("rejected request finalize detached calls=%d want 1", count.detached)
		}
		if f.upstreamCalls.Load() != calls {
			t.Fatal("concurrency rejection reached upstream")
		}
		assertContractDebit(t, before, f.balances(t), 0)
	})
	t.Run("budget_rejection", func(t *testing.T) {
		ctx := context.Background()
		key := contractBalanceKey(f.budget)
		before, calls := f.balances(t), f.upstreamCalls.Load()
		if err := f.deps.Redis.HSet(ctx, key, "balance", 0).Err(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := f.deps.Redis.HSet(ctx, key, "balance", before[1]).Err(); err != nil {
				t.Error(err)
			}
		})
		response, count := f.request(contractBody(false, "budget"), "127.0.0.1:3456")
		if response.Code != 402 {
			t.Fatalf("status=%d body=%s", response.Code, response.Body)
		}
		count.assertWarm(t, 1)
		if f.upstreamCalls.Load() != calls {
			t.Fatal("exhausted API-key budget reached upstream")
		}
		after := f.balances(t)
		if after[0] != before[0] || after[1] != "0" {
			t.Fatalf("budget rejection changed funds: before=%v after=%v", before, after)
		}
		f.assertNoHolds(t)
	})
	for _, scope := range []string{"channel", "credential"} {
		t.Run(scope+"_concurrency_rejection", func(t *testing.T) {
			ctx := context.Background()
			keys := []string{redisx.KeyConcurrencyPrefix + scope + ":1", redisx.KeyConcurrencyPrefix + scope + ":2"}
			for _, key := range keys {
				if err := f.deps.Redis.ZAdd(ctx, key, redis.Z{Score: float64(contractFutureScore()), Member: "fixture-held"}).Err(); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				for _, key := range keys {
					if err := f.deps.Redis.ZRem(ctx, key, "fixture-held").Err(); err != nil {
						t.Error(err)
					}
				}
			})
			before, calls := f.balances(t), f.upstreamCalls.Load()
			response, count := f.request(contractBody(false, "blocked"), "127.0.0.1:3456")
			if response.Code != 429 {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			// Both route candidates reject acquisition, without release calls.
			count.assertWarm(t, 4)
			if f.upstreamCalls.Load() != calls {
				t.Fatal("channel/credential rejection reached upstream")
			}
			assertContractDebit(t, before, f.balances(t), 0)
			f.assertNoHolds(t)
		})
	}
	t.Run("rpm_rejection", func(t *testing.T) {
		ctx := context.Background()
		key := redisx.KeyUserRPMPrefix + "1"
		// These test members join existing admitted requests to reach the cap.
		members := make([]any, 100)
		for i := range members {
			member := "fixture-rpm-" + strconv.Itoa(i)
			members[i] = member
			if err := f.deps.Redis.ZAdd(ctx, key, redis.Z{Score: float64(time.Now().UnixMilli()), Member: member}).Err(); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() {
			if err := f.deps.Redis.ZRem(ctx, key, members...).Err(); err != nil {
				t.Error(err)
			}
		})
		before, calls := f.balances(t), f.upstreamCalls.Load()
		response, count := f.request(contractBody(false, "blocked"), "127.0.0.1:3456")
		if response.Code != 429 {
			t.Fatalf("status=%d body=%s", response.Code, response.Body)
		}
		count.assertWarm(t, 3)
		if f.upstreamCalls.Load() != calls {
			t.Fatal("RPM rejection reached upstream")
		}
		assertContractDebit(t, before, f.balances(t), 0)
		f.assertNoHolds(t)
	})
	t.Run("retry_is_separately_accounted", func(t *testing.T) {
		before, calls := f.balances(t), f.upstreamCalls.Load()
		response, count := f.request(contractBody(false, "retry"), "127.0.0.1:3456")
		if response.Code != 200 {
			t.Fatalf("retry status=%d body=%s", response.Code, response.Body)
		}
		if f.upstreamCalls.Load()-calls != 2 {
			t.Fatal("failover did not execute two real attempts")
		}
		count.assertWarm(t, 6)
		if count.detached != 3 {
			t.Fatalf("retry detached Redis calls=%d want two releases + finalize = 3", count.detached)
		}
		assertContractDebit(t, before, f.balances(t), 20)
		f.assertNoHolds(t)
	})
}

func (f *contractFixture) request(body, remote string) (*httptest.ResponseRecorder, contractResult) {
	ctx := context.WithValue(context.Background(), contractContextKey{}, "request")
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)).WithContext(ctx)
	request.RemoteAddr = remote
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+f.key)
	response := httptest.NewRecorder()
	f.counts.start()
	f.handler.ServeHTTP(response, request)
	return response, f.counts.stop()
}

func (c contractResult) assertWarm(t *testing.T, redisCalls int) {
	t.Helper()
	t.Logf("synchronous PG=%d Redis=%d detached=%d commands=%v", len(c.postgres), len(c.redis), c.detached, c.redis)
	if len(c.postgres) != 0 {
		t.Errorf("warm request executed PostgreSQL: %v", c.postgres)
	}
	if len(c.redis) != redisCalls {
		t.Errorf("Redis round trips=%d want %d commands=%v", len(c.redis), redisCalls, c.redis)
	}
}

func (f *contractFixture) assertNoHolds(t *testing.T) {
	t.Helper()
	for _, id := range []int64{f.wallet, f.budget} {
		value, err := f.deps.Redis.HGet(context.Background(), contractBalanceKey(id), "reserved").Result()
		if err != nil || value != "0" {
			t.Errorf("account %d reserved=%s err=%v", id, value, err)
		}
	}
}

func assertContractDebit(t *testing.T, before, after [2]string, amount int64) {
	t.Helper()
	for i := range before {
		from, err := strconv.ParseInt(before[i], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		to, err := strconv.ParseInt(after[i], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if from-to != amount {
			t.Errorf("account %d debit=%d want %d", i, from-to, amount)
		}
	}
}

func contractBody(stream bool, content string) string {
	return `{"model":"contract-model","messages":[{"role":"user","content":"` + content + `"}],"max_tokens":32,"stream":` + strconv.FormatBool(stream) + `}`
}
