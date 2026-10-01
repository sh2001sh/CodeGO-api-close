//go:build pgintegration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
)

func nativeRequest(f *contractFixture, method, path, body, contentType string, authorized bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:3456"
	r.Header.Set("Content-Type", contentType)
	if authorized {
		r.Header.Set("Authorization", "Bearer "+f.key)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func warmNativeFixture(t *testing.T, f *contractFixture) {
	t.Helper()
	w, _ := f.request(contractBody(false, "warm"), "127.0.0.1:3456")
	if w.Code != 200 {
		t.Fatalf("fixture warmup status=%d body=%s", w.Code, w.Body)
	}
}

func TestProductionNativeRoutesEnforceAuthenticationAndPolicy(t *testing.T) {
	t.Setenv("V3_FILES_DIR", t.TempDir())
	f := newContractFixture(t)
	warmNativeFixture(t, f)
	before, calls := f.balances(t), f.upstreamCalls.Load()
	for _, endpoint := range []struct{ method, path string }{
		{"POST", "/v1/embeddings"}, {"POST", "/v1/images/generations"},
		{"POST", "/v1beta/models/contract-model:embedContent"}, {"GET", "/v1/files"},
		{"GET", "/v1/responses/resp_unknown"}, {"POST", "/v1/responses/resp_unknown/cancel"},
		{"GET", "/v1/realtime"}, {"GET", "/v1/responses"},
		{"POST", "/v1/videos"}, {"POST", "/suno/submit/music"},
	} {
		w := nativeRequest(f, endpoint.method, endpoint.path, `{"model":"contract-model","input":"hello"}`, "application/json", false)
		if w.Code != 401 {
			t.Errorf("unauthorized %s %s status=%d body=%s", endpoint.method, endpoint.path, w.Code, w.Body)
		}
	}
	for _, path := range []string{"/v1/embeddings", "/v1/images/generations", "/v1/videos", "/v1/responses", "/v1beta/models/forbidden:embedContent"} {
		w := nativeRequest(f, "POST", path, `{"model":"forbidden","input":"hello","background":true}`, "application/json", true)
		if w.Code != 403 {
			t.Errorf("forbidden model %s status=%d body=%s", path, w.Code, w.Body)
		}
	}
	for _, path := range []string{"/v1/embeddings", "/v1/videos", "/v1/responses"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"contract-model","input":"hello","background":true}`))
		r.Header.Set("Authorization", "Bearer "+f.key)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		r.RemoteAddr = "198.51.100.7:3456"
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("untrusted forwarded CIDR %s status=%d body=%s", path, w.Code, w.Body)
		}
	}
	if f.upstreamCalls.Load() != calls {
		t.Fatal("rejected native requests reached upstream")
	}
	assertContractDebit(t, before, f.balances(t), 0)
	f.assertNoHolds(t)

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("purpose", "assistants"); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("file", "readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(part, "persistent file content"); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	w := nativeRequest(f, "POST", "/v1/files", body.String(), form.FormDataContentType(), true)
	if w.Code != 200 {
		t.Fatalf("file upload status=%d body=%s", w.Code, w.Body)
	}
	var file struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &file); err != nil || file.ID == "" {
		t.Fatalf("file upload lacks ID: %v", err)
	}
	w = nativeRequest(f, "GET", "/v1/files/"+file.ID+"/content", "", "", true)
	if w.Code != 200 || w.Body.String() != "persistent file content" {
		t.Fatalf("file content status=%d body=%s", w.Code, w.Body)
	}
	w = nativeRequest(f, "DELETE", "/v1/files/"+file.ID, "", "", true)
	if w.Code != 200 {
		t.Fatalf("file delete status=%d", w.Code)
	}
	w = nativeRequest(f, "GET", "/v1/files/"+file.ID+"/content", "", "", true)
	if w.Code != 404 {
		t.Fatalf("deleted file status=%d", w.Code)
	}
}

func TestProductionBackgroundCreationKeepsHoldUntilDurableFinalization(t *testing.T) {
	t.Setenv("V3_FILES_DIR", t.TempDir())
	f := newContractFixture(t)
	warmNativeFixture(t, f)
	ctx := context.Background()
	repository, err := live.NewRedisBackgroundRepository(f.deps.Redis, "", f.deps.Crypto.DeriveKey("background-jobs"))
	if err != nil {
		t.Fatal(err)
	}
	// The fixture owns an initially empty Redis DB15. Delete only keys from the
	// default background repository; its hashed prefix lies outside v3:* cleanup.
	t.Cleanup(func() {
		keys, err := f.deps.Redis.Keys(ctx, "{codego-bg:*}:*").Result()
		if err != nil {
			t.Error(err)
		} else if len(keys) > 0 {
			if err = f.deps.Redis.Del(ctx, keys...).Err(); err != nil {
				t.Error(err)
			}
		}
	})
	store := catalog.NewStore(f.deps.PG.Pool, f.deps.Redis, f.deps.Crypto, runtimeLogger())
	if err = store.Load(ctx); err != nil {
		t.Fatal(err)
	}
	accounts := ledger.NewAccounts(f.deps.PG.Pool)
	settler, err := billing.New(f.deps.Redis, store.Current, accounts, accounts, billing.Config{}, runtimeLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(settler.Close)
	backgroundBilling := billing.NewBackgroundSettler(settler, repository)
	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		before, calls := f.balances(t), f.upstreamCalls.Load()
		w := nativeRequest(f, "POST", path, `{"model":"contract-model","input":"hello","background":true}`, "application/json", true)
		if w.Code != 200 {
			t.Fatalf("background creation %s status=%d body=%s", path, w.Code, w.Body)
		}
		var accepted struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &accepted); err != nil || accepted.Status != "queued" {
			t.Fatalf("invalid queued response: %v body=%s", err, w.Body)
		}
		job, err := repository.GetOwned(ctx, accepted.ID, 1, 1)
		if err != nil || job.Billed || len(job.Reservation) == 0 {
			t.Fatalf("durable job/hold missing: err=%v billed=%v", err, job.Billed)
		}
		for _, id := range []int64{f.wallet, f.budget} {
			reserved, err := f.deps.Redis.HGet(ctx, contractBalanceKey(id), "reserved").Int64()
			if err != nil || reserved <= 0 {
				t.Fatalf("background account %d lacks durable hold: reserved=%d err=%v", id, reserved, err)
			}
		}
		assertContractDebit(t, before, f.balances(t), 0)
		if f.upstreamCalls.Load() != calls {
			t.Fatal("gateway submitted queued background work instead of worker")
		}
		w = nativeRequest(f, "POST", path+"/"+job.ID+"/cancel", "", "application/json", true)
		if w.Code != 200 {
			t.Fatalf("background cancel status=%d body=%s", w.Code, w.Body)
		}
		req := &gateway.Request{ID: job.ID, Body: job.Body, Model: job.Model, PricingHeaders: job.PricingHeaders,
			Principal: gateway.Principal{UserID: job.UserID, KeyID: job.KeyID, Group: job.Group},
			Targets:   []gateway.Target{{ChannelID: job.ChannelID, CredentialID: job.CredentialID}}}
		if err = backgroundBilling.Finalize(ctx, req, job.Reservation, gateway.Outcome{Terminal: gateway.TerminalClientCanceled}); err != nil {
			t.Fatal(err)
		}
		if err = backgroundBilling.Finalize(ctx, req, job.Reservation, gateway.Outcome{Terminal: gateway.TerminalClientCanceled}); err != nil {
			t.Fatalf("restored billing finalization is not idempotent: %v", err)
		}
		assertContractDebit(t, before, f.balances(t), 0)
		f.assertNoHolds(t)
		job, err = repository.Claim(ctx, job.ID, "finalize-"+strconv.FormatInt(time.Now().UnixNano(), 10), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		job.Billed, job.Status = true, "cancelled"
		if err = repository.Save(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
}
