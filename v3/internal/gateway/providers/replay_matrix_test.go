package providers_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

// These fixtures replace only the external provider. Requests still pass through
// parsing, protocol conversion, native transport/decode, retry and finalization.
type replayFixture struct {
	id, name, model, secret, path, contentType                   string
	clientPath, clientBody, doneMarker                           string
	preamble, preambleUsage, content, finish, finishUsage, empty string
	check                                                        func(*testing.T, *http.Request, []byte)
	websocket                                                    bool
}

type replayCase struct {
	name                      string
	terminal                  gateway.Terminal
	charge, estimated, actual bool
	status, attempts          int
}

var replayCases = []replayCase{
	{"completed", gateway.TerminalCompleted, true, false, true, 200, 1},
	{"completed_no_usage", gateway.TerminalCompletedNoUsage, true, true, false, 200, 1},
	{"rate_limited", gateway.TerminalUpstreamErrorBeforeOutput, false, false, false, 429, 2},
	{"truncated_after_output", gateway.TerminalUpstreamErrorAfterOutput, true, true, false, 200, 1},
	{"empty", gateway.TerminalEmptyStream, false, false, false, 502, 2},
	{"client_canceled", gateway.TerminalClientCanceled, true, false, true, 200, 1},
	{"timeout_after_output", gateway.TerminalTimeout, true, true, false, 200, 1},
}

func TestProviderReplayTerminalMatrix(t *testing.T) {
	registry := providers.Registry()
	fixtures := append(compatibleReplayFixtures(), nativeReplayFixtures()...)
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			if registry[fixture.id] == nil {
				t.Fatalf("importable provider %q is absent from the gateway registry", fixture.id)
			}
			for _, tc := range replayCases {
				t.Run(tc.name, func(t *testing.T) { runProviderReplay(t, registry, fixture, tc) })
			}
		})
	}
}

func runProviderReplay(t *testing.T, registry map[string]gateway.Provider, fixture replayFixture, tc replayCase) {
	t.Helper()
	h := newReplayHarness(t, registry, fixture, tc.name)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := `{"model":"public-alias","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	path := "/v1/chat/completions"
	if fixture.clientBody != "" {
		body = fixture.clientBody
	}
	if fixture.clientPath != "" {
		path = fixture.clientPath
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer replay-key")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	reader := sse.NewReader(resp.Body, 0)
	var received strings.Builder
	if tc.name == "client_canceled" {
		for !strings.Contains(received.String(), "hello") {
			ev, err := reader.Next()
			if err != nil {
				t.Fatalf("no semantic content before cancel: %v", err)
			}
			received.Write(ev.Data)
		}
		cancel()
		_ = resp.Body.Close()
		close(h.clientLeft)
	} else {
		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		received.Write(data)
	}
	var outcome gateway.Outcome
	select {
	case outcome = <-h.settler.outcomes:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not finalize native request")
	}
	if resp.StatusCode != tc.status || outcome.Terminal != tc.terminal || outcome.Charge != tc.charge || outcome.Usage.Estimated != tc.estimated {
		t.Errorf("status=%d terminal=%s charge=%v estimated=%v; want %d %s %v %v; response=%s", resp.StatusCode, outcome.Terminal, outcome.Charge, outcome.Usage.Estimated, tc.status, tc.terminal, tc.charge, tc.estimated, received.String())
	}
	if tc.actual && (outcome.Usage.PromptTokens != 10 || outcome.Usage.CompletionTokens != 3) {
		t.Errorf("native usage=%+v; want input=10 output=3", outcome.Usage)
	}
	if tc.estimated && (outcome.Usage.PromptTokens <= 0 || outcome.Usage.CompletionTokens <= 0) {
		t.Errorf("delivered text must produce nonzero estimate: %+v", outcome.Usage)
	}
	if outcome.Delivered != tc.charge {
		t.Errorf("semantic delivery=%v; want %v", outcome.Delivered, tc.charge)
	}
	if !tc.charge && (outcome.Usage.PromptTokens != 0 || outcome.Usage.CompletionTokens != 0 || outcome.Usage.CachedTokens != 0 || len(outcome.Usage.ToolCalls) != 0) {
		t.Error("refunded request has usage")
	}
	if calls := h.calls.Load(); int(calls) != tc.attempts {
		t.Errorf("native requests=%d; want %d", calls, tc.attempts)
	}
	reports := h.planner.results()
	if len(reports) != tc.attempts {
		t.Errorf("attempt reports=%d; want %d", len(reports), tc.attempts)
	}
	if outcome.Target == nil || outcome.Target.ChannelID != int64(tc.attempts) {
		t.Errorf("final channel=%+v; want %d", outcome.Target, tc.attempts)
	}
	if tc.name == "rate_limited" {
		for _, report := range reports {
			if report.OK || !report.Retryable || report.Err == nil || report.Err.Status != 429 {
				t.Errorf("429 retry feedback lost: %+v", report)
			}
		}
	}
	marker := fixture.doneMarker
	if marker == "" {
		marker = "[DONE]"
	}
	if tc.name == "completed" || tc.name == "completed_no_usage" {
		if !strings.Contains(received.String(), "hello") || !strings.Contains(received.String(), marker) {
			t.Errorf("client output/terminal lost: %s", received.String())
		}
		if fixture.clientPath == "/v1/responses" && (!strings.Contains(received.String(), "event: response.output_text.delta") || strings.Contains(received.String(), `"choices"`)) {
			t.Errorf("native Responses event names or response shape lost: %s", received.String())
		}
	}
	if tc.name == "truncated_after_output" || tc.name == "timeout_after_output" {
		code := "upstream_stream_cut"
		if tc.name == "timeout_after_output" {
			code = "upstream_timeout"
		}
		if !strings.Contains(received.String(), code) || strings.Contains(received.String(), marker) {
			t.Errorf("cut stream must expose error without success marker: %s", received.String())
		}
	}
	if !tc.charge && strings.Contains(received.String(), "data:") {
		t.Errorf("failed attempt leaked a lifecycle frame: %s", received.String())
	}
}
