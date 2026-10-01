package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type sampleSink struct {
	service *audit.Service
	record  func(context.Context, audit.Sample) error
}

type blockedSampleLog struct {
	gate chan struct{}
	buf  bytes.Buffer
}

func (w *blockedSampleLog) Write(p []byte) (int, error) {
	<-w.gate
	return w.buf.Write(p)
}

func (s sampleSink) ShouldSample(id string) bool { return s.service.ShouldSample(id) }
func (s sampleSink) RecordSample(ctx context.Context, sample audit.Sample) error {
	return s.record(ctx, sample)
}

func auditTestRequest(id string) *gateway.Request {
	return &gateway.Request{ID: id, Model: "fixture", Received: time.Now(),
		Body: []byte(`{"messages":[{"role":"user","content":"original"}]}`), Principal: gateway.Principal{UserID: 7}}
}

func sampleTestLog(writer io.Writer) *slog.Logger { return slog.New(slog.NewJSONHandler(writer, nil)) }

func auditTestSink(record func(context.Context, audit.Sample) error) sampleSink {
	return sampleSink{service: audit.New(nil, audit.Config{SampleRatePPM: 1_000_000}), record: record}
}

func TestAuditSamplerSnapshotsBackgroundPayloadAndFlushesAcceptedRecords(t *testing.T) {
	var mu sync.Mutex
	var records []audit.Sample
	started, release := make(chan struct{}), make(chan struct{})
	var first sync.Once
	sink := auditTestSink(func(ctx context.Context, sample audit.Sample) error {
		first.Do(func() { close(started) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
		}
		mu.Lock()
		records = append(records, sample)
		mu.Unlock()
		return nil
	})
	s := startAuditSampler(context.Background(), sink, sampleTestLog(io.Discard), 4, 1024)
	t.Cleanup(s.Close)
	req := auditTestRequest("sample-1")
	response := gateway.NewResponseSample(s.MaxBytes())
	response.Add(gateway.Event{Kind: gateway.EventData, Payload: []byte(`{"text":"actual"}`)})
	usage := gateway.Usage{PromptTokens: 3, CompletionTokens: 4, ToolCalls: map[string]int64{"search": 2}}
	s.Record(req, gateway.Outcome{Terminal: gateway.TerminalCompleted, Usage: usage, Charge: true}, response.JSON())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background persistence did not start")
	}
	copy(req.Body, bytes.Repeat([]byte("x"), len(req.Body)))
	usage.ToolCalls["search"] = 900
	s.Record(auditTestRequest("sample-2"), gateway.Outcome{Terminal: gateway.TerminalEmptyStream}, nil)
	close(release)
	s.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(records) != 2 || !bytes.Contains(records[0].Request, []byte("original")) || !bytes.Contains(records[0].Response, []byte(`"search":2`)) {
		t.Fatalf("lost accepted snapshots: %+v", records)
	}
	var recorded struct {
		Terminal string
		Usage    gateway.Usage
		Response sampledResponse
	}
	if err := json.Unmarshal(records[0].Response, &recorded); err != nil {
		t.Fatal(err)
	}
	if recorded.Terminal != "completed" || recorded.Usage.CompletionTokens != 4 || len(recorded.Response.Events) != 1 || !bytes.Contains(recorded.Response.Events[0], []byte(`"text":"actual"`)) {
		t.Fatalf("missing terminal, usage or actual payload: %+v", recorded)
	}
}

func TestAuditSamplerQueueSaturationDoesNotWaitAndReportsDrops(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	sink := auditTestSink(func(ctx context.Context, _ audit.Sample) error {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	})
	logs := &blockedSampleLog{gate: make(chan struct{})}
	var unlock sync.Once
	unlockLogs := func() { unlock.Do(func() { close(logs.gate) }) }
	s := startAuditSampler(context.Background(), sink, sampleTestLog(logs), 1, 1024)
	t.Cleanup(s.Close)
	t.Cleanup(unlockLogs)
	s.Record(auditTestRequest("first"), gateway.Outcome{}, nil)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	s.Record(auditTestRequest("queued"), gateway.Outcome{}, nil)
	returned := make(chan struct{})
	go func() {
		for range 100 {
			s.Record(auditTestRequest("drop"), gateway.Outcome{}, nil)
		}
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("full queue blocked request")
	}
	close(release)
	unlockLogs()
	s.Close()
	if !strings.Contains(logs.buf.String(), "queue_full") || !strings.Contains(logs.buf.String(), `"count":100`) {
		t.Fatalf("saturation was silent or inaccurate: %s", &logs.buf)
	}
}

func TestAuditSamplerCancellationStopsAcceptingAndDoesNotLogSecrets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	s := startAuditSampler(ctx, auditTestSink(func(context.Context, audit.Sample) error {
		return errors.New("storage error containing secret-value")
	}), sampleTestLog(&logs), 2, 1024)
	s.Record(auditTestRequest("accepted"), gateway.Outcome{}, nil)
	cancel()
	s.Close()
	s.Record(auditTestRequest("late"), gateway.Outcome{}, nil)
	if len(s.queue) != 0 || strings.Contains(logs.String(), "secret-value") || !strings.Contains(logs.String(), "closed") || !strings.Contains(logs.String(), "write failed") {
		t.Fatalf("shutdown or secret isolation failed: %s", &logs)
	}
}

func TestAuditSamplerShutdownDrainHasFiniteDeadline(t *testing.T) {
	started := make(chan struct{}, 1)
	s := startAuditSampler(context.Background(), auditTestSink(func(ctx context.Context, _ audit.Sample) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}), sampleTestLog(io.Discard), 1, 1024)
	t.Cleanup(s.Close)
	s.Record(auditTestRequest("blocked"), gateway.Outcome{}, nil)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("storage did not start")
	}
	finished := make(chan struct{})
	go func() { s.Close(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(4 * time.Second):
		t.Fatal("storage outage prevented bounded shutdown")
	}
}

func TestAuditSamplerConfigurationRejectsInvalidValuesEvenWhenDisabled(t *testing.T) {
	for _, name := range []string{"V3_AUDIT_SAMPLE_RATE_PPM", "V3_AUDIT_SAMPLE_QUEUE", "V3_AUDIT_SAMPLE_MAX_BYTES"} {
		t.Setenv(name, "0")
	}
	t.Setenv("V3_AUDIT_SAMPLE_QUEUE", "64")
	t.Setenv("V3_AUDIT_SAMPLE_MAX_BYTES", "1024")
	sampler, closeFn, err := newAuditSampler(context.Background(), nil, nil)
	if err != nil || sampler != nil || closeFn == nil {
		t.Fatalf("default-off: %v %v", sampler, err)
	}
	closeFn()
	for _, tc := range []struct{ name, value string }{
		{"V3_AUDIT_SAMPLE_RATE_PPM", "-1"}, {"V3_AUDIT_SAMPLE_RATE_PPM", "1000001"},
		{"V3_AUDIT_SAMPLE_QUEUE", "0"}, {"V3_AUDIT_SAMPLE_QUEUE", "1025"},
		{"V3_AUDIT_SAMPLE_MAX_BYTES", "1023"}, {"V3_AUDIT_SAMPLE_MAX_BYTES", "1048577"},
		{"V3_AUDIT_SAMPLE_QUEUE", ""}, {"V3_AUDIT_SAMPLE_MAX_BYTES", "secret-value"},
	} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			_, _, err := newAuditSampler(context.Background(), nil, nil)
			if err == nil || !strings.Contains(err.Error(), tc.name) || strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("invalid config accepted or leaked: %v", err)
			}
		})
	}
	t.Setenv("V3_AUDIT_SAMPLE_RATE_PPM", "1000000")
	if _, _, err := newAuditSampler(context.Background(), nil, nil); err == nil {
		t.Fatal("enabled sampler silently accepted absent PostgreSQL")
	}
	service := audit.New(nil, audit.Config{SampleRatePPM: 345_678})
	other := audit.New(nil, audit.Config{SampleRatePPM: 345_678})
	selected := 0
	for i := range 100 {
		id := string(rune('a' + i))
		if service.ShouldSample(id) != other.ShouldSample(id) {
			t.Fatal("selection differs across instances")
		}
		if service.ShouldSample(id) {
			selected++
		}
	}
	if selected == 0 || selected == 100 || service.ShouldSample("") {
		t.Fatal("sample rate or empty ID ignored")
	}
}

func TestAuditSamplerBoundsPersistedEnvelopeIncludingUsage(t *testing.T) {
	response := gateway.NewResponseSample(1024)
	for range 50 {
		response.Add(gateway.Event{Kind: gateway.EventError, Payload: []byte(`{"error":{"code":"actual_error"}}`)})
	}
	usage := gateway.Usage{CompletionTokens: 19, ToolCalls: map[string]int64{strings.Repeat("tool", 400): 1}}
	b := boundedAuditResponse(response.JSON(), gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorAfterOutput, Usage: usage}, 1024)
	var recorded struct {
		Terminal       string
		Usage          gateway.Usage
		UsageTruncated bool `json:"usage_truncated"`
		Response       sampledResponse
	}
	if len(b) > 1024 || json.Unmarshal(b, &recorded) != nil || recorded.Terminal != "upstream_error_after_output" || recorded.Usage.CompletionTokens != 19 || !recorded.UsageTruncated || !recorded.Response.Truncated || len(recorded.Response.Events) == 0 {
		t.Fatalf("bounded accounting lost: (%d) %s", len(b), b)
	}
}
