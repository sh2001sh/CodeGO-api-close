//go:build pgintegration

package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

const productionGuardStateKey = "v3:request-abuse:state:1"
const productionGuardRPMKey = "v3:request-abuse:rpm:1"

func setProductionRestriction(t *testing.T, f *contractFixture, blocked bool, restrictedUntil int64) {
	t.Helper()
	ctx := context.Background()
	_, err := f.deps.PG.Exec(ctx, `INSERT INTO v3_security.account_request_abuse_states(user_id,strikes,restricted_until,last_window_end,blocked,updated_at)
		VALUES(1,1,$1,0,$2,$3) ON CONFLICT(user_id) DO UPDATE SET restricted_until=excluded.restricted_until,blocked=excluded.blocked,updated_at=excluded.updated_at`, restrictedUntil, blocked, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.deps.Redis.Del(ctx, productionGuardStateKey, productionGuardRPMKey).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestProductionRequestGuardRejectsCoreAuxiliaryAndBackground(t *testing.T) {
	t.Setenv("REQUEST_ABUSE_GUARD_ENABLED", "true")
	t.Setenv("V3_FILES_DIR", t.TempDir())
	f := newContractFixture(t)
	warmNativeFixture(t, f)
	before, calls := f.balances(t), f.upstreamCalls.Load()
	ctx := context.Background()
	for _, scenario := range []struct {
		name, code string
		status     int
	}{
		{"blocked", "account_request_disabled", http.StatusForbidden},
		{"restricted", "account_request_rpm_reached", http.StatusTooManyRequests},
		{"corrupt state", "account_request_guard_unavailable", http.StatusServiceUnavailable},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			setProductionRestriction(t, f, scenario.status == http.StatusForbidden, time.Now().Add(time.Hour).Unix())
			if scenario.status == http.StatusTooManyRequests {
				for i := range 10 {
					if err := f.deps.Redis.ZAdd(ctx, productionGuardRPMKey, redis.Z{Score: float64(time.Now().UnixMilli()), Member: fmt.Sprintf("existing-%d", i)}).Err(); err != nil {
						t.Fatal(err)
					}
				}
			}
			if scenario.status == http.StatusServiceUnavailable {
				if err := f.deps.Redis.Set(ctx, productionGuardStateKey, "invalid-private-state", time.Minute).Err(); err != nil {
					t.Fatal(err)
				}
			}
			coreBody := contractBody(false, "guard refusal")
			for _, endpoint := range []struct{ path, body string }{
				{"/v1/chat/completions", coreBody},
				{"/v1/embeddings", `{"model":"contract-model","input":"guard refusal"}`},
				{"/v1/audio/speech", `{"model":"contract-model","input":"guard refusal","voice":"alloy"}`},
				{"/v1/responses", `{"model":"contract-model","input":"guard refusal","background":true}`},
				{"/responses", `{"model":"contract-model","input":"guard refusal","background":true}`},
				{"/backend-api/codex/responses", `{"model":"contract-model","input":"guard refusal","background":true}`},
			} {
				w := nativeRequest(f, http.MethodPost, endpoint.path, endpoint.body, "application/json", true)
				if w.Code != scenario.status || !strings.Contains(w.Body.String(), scenario.code) {
					t.Fatalf("%s status=%d body=%s", endpoint.path, w.Code, w.Body)
				}
				if strings.Contains(w.Body.String(), "invalid-private-state") || strings.Contains(w.Body.String(), "v3_security") {
					t.Fatal("guard disclosed private state")
				}
			}
			if f.upstreamCalls.Load() != calls {
				t.Fatal("guard rejection reached upstream")
			}
			assertContractDebit(t, before, f.balances(t), 0)
			f.assertNoHolds(t)
			keys, err := f.deps.Redis.Keys(ctx, "{codego-bg:*}:*").Result()
			if err != nil || len(keys) != 0 {
				t.Fatalf("guard denial persisted jobs: keys=%v err=%v", keys, err)
			}
		})
	}
}

func TestProductionRequestGuardChargesOneAdmissionAcrossRetry(t *testing.T) {
	t.Setenv("REQUEST_ABUSE_GUARD_ENABLED", "true")
	f := newContractFixture(t)
	warmNativeFixture(t, f)
	setProductionRestriction(t, f, false, time.Now().Add(time.Hour).Unix())
	before, calls := f.balances(t), f.upstreamCalls.Load()
	w, _ := f.request(contractBody(false, "retry"), "127.0.0.1:3456")
	if w.Code != http.StatusOK || f.upstreamCalls.Load()-calls != 2 {
		t.Fatalf("retry status=%d calls=%d body=%s", w.Code, f.upstreamCalls.Load()-calls, w.Body)
	}
	assertContractDebit(t, before, f.balances(t), 20)
	f.assertNoHolds(t)
	ctx := context.Background()
	count, err := f.deps.Redis.ZCard(ctx, productionGuardRPMKey).Result()
	if err != nil || count != 1 {
		t.Fatalf("retry guard admissions=%d err=%v", count, err)
	}
	f.counts.start()
	w = nativeRequest(f, http.MethodGet, "/v1/models", "", "", true)
	counts := f.counts.stop()
	if w.Code != http.StatusOK || len(counts.postgres) != 0 || len(counts.redis) != 0 {
		t.Fatalf("metadata read touched guard: status=%d PG=%v Redis=%v", w.Code, counts.postgres, counts.redis)
	}
	count, err = f.deps.Redis.ZCard(ctx, productionGuardRPMKey).Result()
	if err != nil || count != 1 {
		t.Fatalf("metadata read consumed relay admission: count=%d err=%v", count, err)
	}
}
