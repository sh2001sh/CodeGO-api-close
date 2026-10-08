package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type summarySink func(context.Context, RequestRecord) error

func (f summarySink) RecordRequest(ctx context.Context, r RequestRecord) error {
	return f(ctx, r)
}

func TestRequestSummarySafelyObservesConcurrentSettlementMetadata(t *testing.T) {
	s := startRequestRecorder(context.Background(), summarySink(func(_ context.Context, r RequestRecord) error {
		if r.Amount < 0 || r.Amount > 1000 {
			return errors.New("invalid concurrent amount")
		}
		return nil
	}), requestTestLog(io.Discard), 2048)
	req := summaryRequest("concurrent")
	start, done := make(chan struct{}), make(chan struct{})
	go func() {
		<-start
		for n := int64(0); n <= 1000; n++ {
			atomic.StoreInt64(&req.SettledAmount, n)
		}
		close(done)
	}()
	close(start)
	for range 1000 {
		s.RecordRequest(req, gateway.Outcome{Terminal: gateway.TerminalCompleted}, true)
	}
	<-done
	s.Close()
	if s.failed.Load() != 0 || s.written.Load() != 1000 || s.dropped.Load() != 0 {
		t.Fatalf("concurrent observation lost metadata: written=%d failed=%d dropped=%d", s.written.Load(), s.failed.Load(), s.dropped.Load())
	}
}

func summaryRequest(id string) *gateway.Request {
	return &gateway.Request{ID: id, Model: "test-model", Protocol: gateway.ProtocolOpenAIChat, Received: time.Now(),
		Principal: gateway.Principal{UserID: 1, KeyID: 2, Group: "default"}, Body: []byte(`{"content":"private-prompt"}`)}
}

func TestRequestSummarySnapshotsOnlyMetadataAndFlushes(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var records []RequestRecord
	s := startRequestRecorder(context.Background(), summarySink(func(ctx context.Context, r RequestRecord) error {
		once.Do(func() { close(started) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
		}
		records = append(records, r)
		return nil
	}), requestTestLog(io.Discard), 8)
	t.Cleanup(s.Close)
	r := summaryRequest("first")
	target := gateway.Target{ChannelID: 9, Group: "actual-group", Secret: "private-credential"}
	s.RecordRequest(r, gateway.Outcome{Terminal: gateway.TerminalCompleted, Charge: true, Usage: gateway.Usage{PromptTokens: 10}, Target: &target}, true)
	<-started
	r.Model, target.Group, r.Body = "changed", "changed", []byte("changed")
	s.RecordRequest(summaryRequest("failed"), gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput, Err: &gateway.UpstreamError{Status: 502, Code: "upstream_unavailable", Message: "private-error", Body: []byte("private-response")}}, true)
	close(release)
	s.Close()
	if len(records) != 2 || records[0].Group != "actual-group" || records[0].Model != "test-model" || records[0].ChannelID != 9 || !records[0].Billable || records[1].Status != "failed" || !records[1].Counted {
		t.Fatalf("lost terminal metadata: %+v", records)
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-prompt", "private-credential", "private-error", "private-response"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatal("metadata captured a payload or secret")
		}
	}
	if s.written.Load() != 2 || s.failed.Load() != 0 || s.dropped.Load() != 0 {
		t.Fatal("incorrect persistence counters")
	}
}

func TestRequestSummaryFullQueueNeverWaitsForStorageOrLogs(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	logs := &blockedRequestLog{gate: make(chan struct{})}
	s := startRequestRecorder(context.Background(), summarySink(func(ctx context.Context, r RequestRecord) error {
		once.Do(func() { close(started) })
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}), requestTestLog(logs), 1)
	s.RecordRequest(summaryRequest("first"), gateway.Outcome{}, false)
	<-started
	s.RecordRequest(summaryRequest("queued"), gateway.Outcome{}, false)
	returned := make(chan struct{})
	go func() {
		for range 100 {
			s.RecordRequest(summaryRequest("dropped"), gateway.Outcome{}, false)
		}
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("full queue blocked the request path")
	}
	close(release)
	close(logs.gate)
	s.Close()
	if s.dropped.Load() != 100 || s.written.Load() != 2 || !bytes.Contains(logs.buf.Bytes(), []byte(`"dropped":100`)) {
		t.Fatalf("lost records were not surfaced: dropped=%d written=%d logs=%s", s.dropped.Load(), s.written.Load(), &logs.buf)
	}
	s.RecordRequest(summaryRequest("after-close"), gateway.Outcome{}, false)
	if s.dropped.Load() != 101 {
		t.Fatal("closed queue silently lost a summary")
	}
}

func TestRequestSummaryFailedStorageIsExplicitAndShutdownIsFinite(t *testing.T) {
	var logs bytes.Buffer
	s := startRequestRecorder(context.Background(), summarySink(func(ctx context.Context, r RequestRecord) error {
		return errors.New("storage-private-value")
	}), requestTestLog(&logs), 4)
	s.RecordRequest(summaryRequest("failed"), gateway.Outcome{}, false)
	s.Close()
	if s.failed.Load() != 1 || !bytes.Contains(logs.Bytes(), []byte(`"storage_failures":1`)) || bytes.Contains(logs.Bytes(), []byte("storage-private-value")) {
		t.Fatalf("storage failure was silent or exposed details: %s", &logs)
	}
	ctx, cancel := context.WithCancel(context.Background())
	blocked := startRequestRecorder(ctx, summarySink(func(ctx context.Context, r RequestRecord) error {
		<-ctx.Done()
		return ctx.Err()
	}), requestTestLog(io.Discard), 4)
	for range 4 {
		blocked.RecordRequest(summaryRequest("queued"), gateway.Outcome{}, false)
	}
	cancel()
	start := time.Now()
	blocked.Close()
	if time.Since(start) > requestRecordTimeout+time.Second || blocked.written.Load() != 0 || blocked.failed.Load()+blocked.dropped.Load() != 4 {
		t.Fatalf("shutdown failed to bound or account for lost records: failed=%d dropped=%d", blocked.failed.Load(), blocked.dropped.Load())
	}
	reg := prometheus.NewRegistry()
	blocked.Register(reg)
	families, err := reg.Gather()
	if err != nil || len(families) != 4 {
		t.Fatalf("missing operational counters: %d %v", len(families), err)
	}
}
