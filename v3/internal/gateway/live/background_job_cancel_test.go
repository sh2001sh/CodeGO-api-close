package live

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestBackgroundJobsQueuedCancelRefundsWithoutUpstream(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer up.Close()
	h, mux, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
	if w := backgroundCall(mux, "POST", "/responses/"+id+"/cancel", "other-key"); w.Code != 404 {
		t.Fatal("another API key canceled the job")
	}
	for i := 0; i < 2; i++ {
		if w := backgroundCall(mux, "POST", "/responses/"+id+"/cancel", "owner"); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	out, count, _ := billing.result(id)
	if calls.Load() != 0 || job.Status != "cancelled" || !job.Billed || out.Charge || out.Terminal != gateway.TerminalClientCanceled || count != 1 {
		t.Fatalf("queued cancel job=%+v out=%+v calls=%d", job, out, calls.Load())
	}
	if w := backgroundCall(mux, "GET", "/responses/"+id+"?stream=true", "owner"); w.Code != 400 {
		t.Fatal("nonstream creation resumed as stream")
	}
}

func TestBackgroundJobsAcceptedNativeCancelConsumesPartialUsage(t *testing.T) {
	var creates, cancels atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/cancel") {
			cancels.Add(1)
			_, _ = io.WriteString(w, `{"id":"resp_up","object":"response","status":"cancelled","output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":4,"output_tokens":2}}`)
		} else {
			creates.Add(1)
			_, _ = io.WriteString(w, `{"id":"resp_up","object":"response","status":"queued","output":[]}`)
		}
	}))
	defer up.Close()
	h, mux, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	id := createBackgroundForTest(t, wrapped, "/v1/responses", `{"model":"gpt-test","background":true}`)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if w := backgroundCall(mux, "POST", "/backend-api/codex/responses/"+id+"/cancel", "owner"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	repo.expire(id)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	out, count, _ := billing.result(id)
	if !out.Charge || out.Terminal != gateway.TerminalClientCanceled || out.Usage.CompletionTokens != 2 || count != 1 || creates.Load() != 1 || cancels.Load() != 1 {
		t.Fatalf("accepted cancel incorrectly refunded: out=%+v count=%d creates=%d cancels=%d", out, count, creates.Load(), cancels.Load())
	}
}

func TestBackgroundJobsActiveLocalCancelAndExclusiveClaims(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Split(backgroundLocalEvents, "event: response.completed")[0])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer up.Close()
	h, mux, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "codex")
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
	finished := make(chan error, 1)
	go func() { finished <- h.Reconcile(context.Background(), 10) }()
	for {
		select {
		case name := <-repo.appended:
			if name == "response.output_text.delta" {
				goto active
			}
		case <-time.After(time.Second):
			t.Fatal("local output did not become durable")
		}
	}
active:
	restarted, _ := New(h.cfg)
	if err := restarted.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("exclusive job lease allowed duplicate upstream execution")
	}
	if w := backgroundCall(mux, "POST", "/responses/"+id+"/cancel", "owner"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("active cancellation did not stop local work")
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	out, count, _ := billing.result(id)
	if !job.Billed || job.Status != "cancelled" || !out.Charge || !out.Usage.Estimated || out.Usage.CompletionTokens != 2 || count != 1 || calls.Load() != 1 {
		t.Fatalf("partial cancel job=%+v out=%+v count=%d", job, out, count)
	}
}

func TestBackgroundJobsRevokedModelStillAllowsMetadataAndCancel(t *testing.T) {
	h, mux, wrapped, _, _ := backgroundJobsFixture(t, "http://unused.invalid", "openai")
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
	h.cfg.Auth = backgroundJobsAuthFunc(func(context.Context, string) (gateway.Principal, error) {
		return gateway.Principal{UserID: 1, KeyID: 11, AllowedModels: []string{}}, nil
	})
	for _, method := range []string{"GET", "POST"} {
		path := "/responses/" + id
		if method == "POST" {
			path += "/cancel"
		}
		if w := backgroundCall(mux, method, path, "owner"); w.Code != 200 {
			t.Fatalf("metadata denied after model revoke: %s %s", method, w.Body.String())
		}
	}
}
