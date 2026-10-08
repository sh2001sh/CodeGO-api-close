package live

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type backgroundRequestRecords struct {
	count, attempts int64
	group           string
	settled         bool
	usage           gateway.Usage
}

func (r *backgroundRequestRecords) RecordRequest(req *gateway.Request, out gateway.Outcome, settled bool) {
	r.count++
	r.attempts, r.settled, r.usage = req.PersistedAttempts, settled, out.Usage
	if out.Target != nil {
		r.group = out.Target.Group
	}
}

func TestBackgroundMetadataWaitsForAcceptedSettlementAndKeepsFrozenTarget(t *testing.T) {
	var submissions atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		submissions.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, backgroundCompletedSnapshot)
	}))
	defer upstream.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, upstream.URL, "openai")
	records := &backgroundRequestRecords{}
	h.cfg.Requests = records
	id := createBackgroundForTest(t, wrapped, "/v1/responses", `{"model":"gpt-test","background":true}`)
	repo.change(id, func(job *BackgroundJob) { job.TargetGroup = "frozen-target-group" })
	billing.finalizeErr = errors.New("temporary settlement failure")
	if err := h.Reconcile(context.Background(), 10); err == nil {
		t.Fatal("expected real settlement failure")
	}
	if records.count != 0 {
		t.Fatal("unaccepted settlement emitted terminal metadata")
	}
	billing.finalizeErr = nil
	repo.expire(id)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if records.count != 1 || !records.settled || records.group != "frozen-target-group" || records.attempts != 1 || records.usage.PromptTokens != 5 || records.usage.CompletionTokens != 3 {
		t.Fatalf("metadata differs after settlement retry: %+v", records)
	}
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if records.count != 1 || submissions.Load() != 1 {
		t.Fatal("terminal retry resubmitted or emitted another logical result")
	}
}
