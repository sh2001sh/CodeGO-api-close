package audit

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestRequestPerformanceOnlyMeasuresStreamingOutputAndReportedTokens(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		stream, delivered, estimated bool
		terminal                     gateway.Terminal
		tokens                       int64
		ttft, generation             time.Duration
		wantTTFT, wantGeneration     bool
	}{
		{"real", true, true, false, gateway.TerminalCompleted, 12, 1200 * time.Microsecond, 300 * time.Millisecond, true, true},
		{"estimated", true, true, true, gateway.TerminalCompletedNoUsage, 12, time.Millisecond, time.Second, true, false},
		{"interrupted", true, true, false, gateway.TerminalUpstreamErrorAfterOutput, 12, time.Millisecond, time.Second, true, false},
		{"cancelled", true, true, false, gateway.TerminalClientCanceled, 12, time.Millisecond, time.Second, true, false},
		{"buffered", false, true, false, gateway.TerminalCompleted, 12, time.Second, time.Second, false, false},
		{"no_output", true, false, false, gateway.TerminalEmptyStream, 12, 0, 0, false, false},
		{"no_tokens", true, true, false, gateway.TerminalCompleted, 0, time.Millisecond, time.Second, true, false},
		{"unknown_timing", true, true, false, gateway.TerminalCompleted, 12, 0, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := summaryRequest("performance")
			req.Stream = tc.stream
			out := gateway.Outcome{Terminal: tc.terminal, Delivered: tc.delivered, TTFT: tc.ttft, Generation: tc.generation,
				Usage: gateway.Usage{CompletionTokens: tc.tokens, Estimated: tc.estimated}}
			r := ProjectRequestRecord(req, out, false)
			if (r.TTFTMS != nil) != tc.wantTTFT || (r.GenerationMS != nil) != tc.wantGeneration || r.CompletionTokens != tc.tokens {
				t.Fatalf("timing/throughput availability or accounting tokens changed: %+v", r)
			}
			if tc.wantTTFT && *r.TTFTMS != float64(tc.ttft)/float64(time.Millisecond) {
				t.Fatalf("TTFT lost submillisecond precision: %v", *r.TTFTMS)
			}
			if tc.wantGeneration && *r.GenerationMS != float64(tc.generation)/float64(time.Millisecond) {
				t.Fatalf("generation timing mismatch: %v", *r.GenerationMS)
			}
		})
	}
}

func TestRequestPerformanceRejectsNonfiniteAndUnusableSamples(t *testing.T) {
	now := time.Now()
	base := RequestRecord{RequestID: "r", Model: "model", UserID: 1, KeyID: 2, StartedAt: now, CompletedAt: now}
	for _, value := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		r := base
		r.TTFTMS = &value
		if err := New(nil, Config{}).RecordRequest(context.Background(), r); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid performance sample %v accepted: %v", value, err)
		}
	}
	generation := 100.0
	base.GenerationMS = &generation
	if err := New(nil, Config{}).RecordRequest(context.Background(), base); !errors.Is(err, ErrInvalid) {
		t.Fatalf("generation without TTFT/tokens accepted: %v", err)
	}
}
