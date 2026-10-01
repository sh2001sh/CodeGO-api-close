package live

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestBackgroundJobsCreateNativeAndRecoverAfterRestart(t *testing.T) {
	var posts, gets atomic.Int64
	var rotated atomic.Bool
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			posts.Add(1)
			body, _ := io.ReadAll(r.Body)
			if !gjson.GetBytes(body, "background").Bool() || !gjson.GetBytes(body, "stream").Bool() {
				t.Error("native background request lost flags")
			}
			_, _ = io.WriteString(w, `{"id":"resp_up","object":"response","status":"queued","output":[]}`)
		} else {
			gets.Add(1)
			if r.URL.Path != "/v1/responses/resp_up" || r.URL.Query().Get("starting_after") != "-1" {
				t.Error("native restart lost route/cursor")
			}
			if r.Header.Get("Authorization") == "Bearer rotated-secret" {
				rotated.Store(true)
			}
			_, _ = io.WriteString(w, backgroundCompletedSnapshot)
		}
	}))
	defer up.Close()
	h, mux, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true,"input":"hello"}`)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	if job.UpstreamID != "resp_up" || job.Billed {
		t.Fatalf("acceptance prematurely billed: %+v", job)
	}
	if _, count, _ := billing.result(id); count != 0 {
		t.Fatal("queued acceptance was finalized")
	}
	// New handler/process shares only the persisted job and durable billing JSON.
	cfg := h.cfg
	cfg.Resolve = func(context.Context, int64, int64) (gateway.Target, error) {
		return gateway.Target{ChannelID: 7, CredentialID: 9, Provider: "openai", BaseURL: up.URL, Secret: "rotated-secret"}, nil
	}
	restarted, _ := New(cfg)
	repo.expire(id)
	if err := restarted.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	job, _ = repo.GetOwned(context.Background(), id, 1, 11)
	out, count, _ := billing.result(id)
	if !job.Billed || job.Status != "completed" || !out.Charge || out.Usage.PromptTokens != 5 || out.Usage.CompletionTokens != 3 || out.Usage.CachedTokens != 2 || count != 1 || posts.Load() != 1 || gets.Load() != 1 || !rotated.Load() {
		t.Fatalf("restart/settlement mismatch job=%+v out=%+v count=%d posts=%d gets=%d rotated=%v", job, out, count, posts.Load(), gets.Load(), rotated.Load())
	}
	for _, key := range []string{"other-user", "other-key"} {
		if w := backgroundCall(mux, "GET", "/responses/"+id, key); w.Code != 404 {
			t.Errorf("job leaked to %s", key)
		}
	}
	w := backgroundCall(mux, "GET", "/responses/"+id, "owner")
	if gjson.GetBytes(w.Body.Bytes(), "id").Str != id || gjson.GetBytes(w.Body.Bytes(), "status").Str != "completed" {
		t.Fatalf("lookup=%s", w.Body.String())
	}
}

func TestBackgroundJobsNativeStreamResumeAndFailureKeepsReportedUsage(t *testing.T) {
	var posts, gets atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if r.Method == "POST" {
			posts.Add(1)
			_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":4,\"response\":{\"id\":\"resp_up\",\"status\":\"in_progress\"}}\n\nevent: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":5,\"delta\":\"partial\",\"response_id\":\"resp_up\"}\n\n")
		} else {
			gets.Add(1)
			if r.URL.Query().Get("starting_after") != "5" {
				t.Errorf("resume cursor %q", r.URL.Query().Get("starting_after"))
			}
			// Replay the last event to prove the durable source cursor suppresses it.
			_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":5,\"delta\":\"partial\"}\n\nevent: response.failed\ndata: {\"type\":\"response.failed\",\"sequence_number\":6,\"response\":{\"id\":\"resp_up\",\"status\":\"failed\",\"output\":[],\"error\":{\"code\":\"upstream_failure\"},\"usage\":{\"input_tokens\":8,\"output_tokens\":4}}}\n\n")
		}
	}))
	defer up.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	repo.expire(id)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	events, _ := repo.Events(context.Background(), id, -1, 100)
	out, count, _ := billing.result(id)
	if posts.Load() != 1 || gets.Load() != 1 || len(events) != 3 || job.GeneratedBytes != 7 || job.LastUpstreamSequence != 6 || !out.Charge || out.Terminal != gateway.TerminalUpstreamErrorAfterOutput || out.Usage.PromptTokens != 8 || out.Usage.CompletionTokens != 4 || count != 1 {
		t.Fatalf("native resume job=%+v out=%+v events=%d posts=%d gets=%d", job, out, len(events), posts.Load(), gets.Load())
	}
}

func TestBackgroundJobsNativeToolPartialUsagePreservesItemEvents(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"id\":\"resp_up\",\"status\":\"in_progress\"}}\n\nevent: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"sequence_number\":1,\"output_index\":0,\"item\":{\"type\":\"web_search_call\",\"id\":\"search_1\"}}\n\nevent: response.cancelled\ndata: {\"type\":\"response.cancelled\",\"sequence_number\":2,\"response\":{\"id\":\"resp_up\",\"status\":\"cancelled\",\"output\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n")
	}))
	defer up.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true,"tools":[{"type":"web_search_preview"}]}`)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	events, _ := repo.Events(context.Background(), id, -1, 100)
	out, _, _ := billing.result(id)
	if !out.Charge || out.Usage.ToolCalls["web_search_preview"] != 1 || gjson.GetBytes(events[1].Payload, "item.id").Str != "search_1" {
		t.Fatalf("tool event or partial pricing lost: out=%+v event=%s", out, events[1].Payload)
	}
}

func TestBackgroundJobsNativeEmptyCompletedRefunds(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":"resp_up","object":"response","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":1}}`)
	}))
	defer up.Close()
	h, _, wrapped, _, billing := backgroundJobsFixture(t, up.URL, "openai")
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	out, _, _ := billing.result(id)
	if out.Charge || out.Terminal != gateway.TerminalEmptyStream {
		t.Fatalf("empty terminal charged: %+v", out)
	}
}
