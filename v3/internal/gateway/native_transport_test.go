package gateway_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bridge"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

type nativeTransport struct {
	openai.Provider
	calls atomic.Int32
	fail  bool
}

func (p *nativeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	p.calls.Add(1)
	_ = req.Body.Close()
	if p.fail {
		return nil, errors.New("native transport failed")
	}
	wire := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"native\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(wire)), Request: req}, nil
}

func TestProviderNativeTransportSurvivesProtocolBridge(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "bridge"}[wrapped], func(t *testing.T) {
			native := &nativeTransport{}
			var adapter gateway.Provider = native
			if wrapped {
				adapter = bridge.Provider{Chat: native}
			}
			h := nativeHarness(t, adapter, "native", "upstream HTTP must not run")
			view := h.do(streamBody)
			if native.calls.Load() != 1 || view.status != 200 || !strings.Contains(strings.Join(view.data, ""), "native") {
				t.Fatalf("native calls=%d response=%+v", native.calls.Load(), view)
			}
			out := h.outcome()
			if !out.Charge || out.Usage.CompletionTokens != 2 || out.Terminal != gateway.TerminalCompletedNoUsage {
				t.Fatalf("outcome=%+v", out)
			}
		})
	}
}

func TestProviderNativeTransportFailureRefunds(t *testing.T) {
	native := &nativeTransport{fail: true}
	h := nativeHarness(t, bridge.Provider{Chat: native}, "native", "upstream HTTP must not run")
	view := h.do(streamBody)
	if view.status != http.StatusBadGateway || native.calls.Load() != 1 {
		t.Fatalf("native calls=%d response=%+v", native.calls.Load(), view)
	}
	if out := h.outcome(); out.Charge {
		t.Fatalf("charged failed native transport: %+v", out)
	}
}
