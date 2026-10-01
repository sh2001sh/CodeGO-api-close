package live

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundJobsCrashAfterTerminalAppendRestoresBeforePoll(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer up.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
	repo.change(id, func(job *BackgroundJob) {
		job.Status = "in_progress"
		job.UpstreamID = "resp_up"
		job.LastUpstreamSequence = 1
	})
	// Simulate an accepted terminal event whose Append committed but Save did
	// not. The stored job still looks running when the replacement worker starts.
	payload, _ := json.Marshal(map[string]any{"type": "response.completed", "sequence_number": 2, "response": json.RawMessage(backgroundCompletedSnapshot)})
	repo.mu.Lock()
	repo.events[id] = []BackgroundEvent{{Sequence: 0, Type: "response.completed", Payload: payload}}
	repo.mu.Unlock()
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	out, count, _ := billing.result(id)
	if calls.Load() != 0 || !job.Billed || job.Status != "completed" || job.LastUpstreamSequence != 2 || !out.Charge || out.Usage.CompletionTokens != 3 || count != 1 {
		t.Fatalf("durable terminal event lost job=%+v out=%+v calls=%d", job, out, calls.Load())
	}
}

func TestBackgroundJobsUnknownAcceptanceNeverResubmits(t *testing.T) {
	for _, mode := range []string{"malformed", "transport", "server-error"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch mode {
				case "malformed":
					_, _ = io.WriteString(w, "accepted but not JSON")
				case "transport":
					conn, _, _ := w.(http.Hijacker).Hijack()
					_ = conn.Close()
				case "server-error":
					w.WriteHeader(500)
				}
			}))
			defer up.Close()
			h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
			id := createBackgroundForTest(t, wrapped, "/v1/responses", `{"model":"gpt-test","background":true}`)
			if err := h.Reconcile(context.Background(), 10); !errors.Is(err, errBackgroundUnknown) {
				t.Fatalf("ambiguous submit not reported: %v", err)
			}
			repo.expire(id)
			restarted, _ := New(h.cfg)
			if err := restarted.Reconcile(context.Background(), 10); !errors.Is(err, errBackgroundUnknown) {
				t.Fatalf("unknown hold not retained: %v", err)
			}
			job, _ := repo.GetOwned(context.Background(), id, 1, 11)
			if _, count, _ := billing.result(id); count != 0 || job.Billed || job.Status != "unknown_acceptance" || calls.Load() != 1 {
				t.Fatalf("ambiguous request retried/refunded: job=%+v count=%d calls=%d", job, count, calls.Load())
			}
		})
	}
}

func TestBackgroundJobsCrashBeforeKnownIDDoesNotSubmitAgain(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer up.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	id := createBackgroundForTest(t, wrapped, "/v1/responses", `{"model":"gpt-test","background":true}`)
	repo.change(id, func(job *BackgroundJob) { job.Status = "submitting" })
	if err := h.Reconcile(context.Background(), 10); !errors.Is(err, errBackgroundUnknown) {
		t.Fatal(err)
	}
	if _, count, _ := billing.result(id); count != 0 || calls.Load() != 0 {
		t.Fatal("crashed ambiguous submission was replayed or refunded")
	}
}

func TestBackgroundJobsBillingCrashIsIdempotent(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, backgroundCompletedSnapshot)
	}))
	defer up.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	id := createBackgroundForTest(t, wrapped, "/v1/responses", `{"model":"gpt-test","background":true}`)
	repo.failBilledSave = true
	if err := h.Reconcile(context.Background(), 10); err == nil {
		t.Fatal("simulated crash was hidden")
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	if job.Billed || job.Status != "completed" {
		t.Fatalf("terminal state not durable before posting: %+v", job)
	}
	repo.expire(id)
	restarted, _ := New(h.cfg)
	if err := restarted.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	out, count, attempts := billing.result(id)
	job, _ = repo.GetOwned(context.Background(), id, 1, 11)
	if count != 1 || attempts != 2 || calls.Load() != 1 || !job.Billed || !out.Charge {
		t.Fatalf("billing replay count=%d attempts=%d calls=%d job=%+v", count, attempts, calls.Load(), job)
	}
}

func TestBackgroundJobsLeaseLossNeverFinalizesFromOldWorker(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Split(backgroundLocalEvents, "event: response.completed")[0])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer up.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "codex")
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
			t.Fatal("output was not persisted")
		}
	}
active:
	repo.change(id, func(job *BackgroundJob) { job.LeaseID = "other-worker"; job.LeaseUntil = time.Now().Add(time.Minute) })
	select {
	case err := <-finished:
		if !errors.Is(err, ErrBackgroundLeaseConflict) {
			t.Fatalf("lease loss hidden: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lease loss did not stop upstream")
	}
	if _, count, _ := billing.result(id); count != 0 {
		t.Fatal("old worker finalized after lease ownership was lost")
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	if job.Status != "in_progress" || job.Billed {
		t.Fatal("old worker overwrote durable owner state")
	}
	// Restarted local fallback cannot replay store=false; settle saved partial
	// output once, rather than start the generation a second time.
	repo.expire(id)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	out, count, _ := billing.result(id)
	if !out.Charge || out.Usage.CompletionTokens != 2 || count != 1 {
		t.Fatalf("partial restart usage lost: %+v", out)
	}
}
