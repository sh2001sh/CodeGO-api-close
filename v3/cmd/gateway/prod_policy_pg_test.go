//go:build pgintegration

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestProductionSnapshotPolicyRejectsBeforeAdmission(t *testing.T) {
	t.Setenv("V3_FILES_DIR", t.TempDir())
	f := newContractFixture(t)
	ctx := context.Background()
	if _, err := f.deps.PG.Exec(ctx, `INSERT INTO v3_platform.settings(key,value) VALUES('SensitiveWords','["contains:fixture-snapshot-block"]')`); err != nil {
		t.Fatal(err)
	}
	if err := catalog.NewPublisher(f.deps.PG.Pool, f.deps.Redis, f.deps.Crypto, f.deps.Crypto, runtimeLogger()).PublishNow(ctx); err != nil {
		t.Fatal(err)
	}
	// Construct from the published version to distinguish the real snapshot
	// callback from an unwired policy using only its built-in defaults.
	background := context.WithValue(ctx, contractContextKey{}, contractBackground)
	handler, closeFn, err := prodHandler(background, f.deps, runtimeLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFn)
	f.handler = handler
	warmNativeFixture(t, f)
	before, calls := f.balances(t), f.upstreamCalls.Load()
	coreBody := `{"model":"contract-model","messages":[{"role":"user","content":"fixture-snapshot-block"}],"input":"fixture-snapshot-block","background":true}`
	for _, scenario := range []struct{ path, body string }{
		{"/v1/chat/completions", coreBody}, {"/v1/responses", coreBody},
		{"/responses", coreBody}, {"/backend-api/codex/responses", coreBody},
		{"/v1/embeddings", `{"model":"contract-model","input":"fixture-snapshot-block"}`},
		{"/v1/audio/speech", `{"model":"contract-model","input":"fixture-snapshot-block","voice":"alloy"}`},
		{"/v1beta/models/contract-model:embedContent", `{"content":{"parts":[{"text":"fixture-snapshot-block"}]}}`},
	} {
		f.counts.start()
		w := nativeRequest(f, http.MethodPost, scenario.path, scenario.body, "application/json", true)
		counts := f.counts.stop()
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "sensitive_words_detected") {
			t.Fatalf("snapshot policy %s status=%d body=%s", scenario.path, w.Code, w.Body)
		}
		if len(counts.postgres) != 0 || len(counts.redis) != 0 {
			t.Fatalf("policy rejection %s touched PG=%v Redis=%v", scenario.path, counts.postgres, counts.redis)
		}
	}
	if f.upstreamCalls.Load() != calls {
		t.Fatal("blocked production request reached upstream")
	}
	assertContractDebit(t, before, f.balances(t), 0)
	f.assertNoHolds(t)
	keys, err := f.deps.Redis.Keys(ctx, "{codego-bg:*}:*").Result()
	if err != nil || len(keys) != 0 {
		t.Fatalf("blocked prompt persisted background jobs: keys=%v err=%v", keys, err)
	}
}

func TestProductionSignedBatchRequestIDKeepsAuthorizationAndBilling(t *testing.T) {
	f := newContractFixture(t)
	warmNativeFixture(t, f)
	id := "market-test-" + strings.Repeat("a", 32)
	body := contractBody(false, "signed batch")
	authorization := "Bearer " + f.key
	secret := f.deps.Crypto.DeriveKey("market-batch")
	digest := sha256.Sum256([]byte(body))
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%x", id, authorization, digest)
	signature := hex.EncodeToString(mac.Sum(nil))
	for _, valid := range []bool{true, false} {
		before, calls := f.balances(t), f.upstreamCalls.Load()
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:3456"
		r.Header.Set("Authorization", authorization)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Market-Batch-Request-ID", id)
		value := signature
		if !valid {
			value = strings.Repeat("0", 64)
		}
		r.Header.Set("X-Market-Batch-Signature", value)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || (w.Header().Get("X-Request-Id") == id) != valid {
			t.Fatalf("valid=%v status=%d request_id=%s", valid, w.Code, w.Header().Get("X-Request-Id"))
		}
		if f.upstreamCalls.Load()-calls != 1 {
			t.Fatal("signed ID bypassed or duplicated upstream")
		}
		assertContractDebit(t, before, f.balances(t), 20)
		f.assertNoHolds(t)
	}
}
