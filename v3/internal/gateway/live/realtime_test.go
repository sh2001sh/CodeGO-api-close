package live

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func TestRealtimeBinaryFramesAndDeduplicatedUsage(t *testing.T) {
	frames := make(chan wireFrame, 1)
	upstream := httptest.NewServer(websocket.Server{Handshake: func(cfg *websocket.Config, r *http.Request) error {
		if r.URL.Path != "/v1/realtime" || r.URL.Query().Get("model") != "upstream-model" {
			t.Errorf("upstream URL=%s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Error("wrong upstream secret")
		}
		return nil
	}, Handler: websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		var f wireFrame
		if err := frameCodec.Receive(conn, &f); err != nil {
			return
		}
		frames <- f
		for i := 0; i < 2; i++ {
			_ = websocket.Message.Send(conn, `{"type":"response.done","response":{"id":"resp_rt","output":[{"type":"message"}],"usage":{"input_tokens":20,"output_tokens":8,"input_token_details":{"cached_tokens":3,"audio_tokens":6},"output_token_details":{"audio_tokens":4}}}}`)
		}
		var tail wireFrame
		_ = frameCodec.Receive(conn, &tail)
	})})
	defer upstream.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 2)}
	_, server, _ := socketFixture(t, []gateway.Target{{ChannelID: 1, CredentialID: 2, Provider: "openai", BaseURL: upstream.URL, UpstreamModel: "upstream-model", Secret: "upstream-secret"}}, ledger)
	conn := dialSocket(t, server.URL, "/v1/realtime?model=gpt-realtime")
	payload := []byte{0, 1, 255, 4}
	_ = frameCodec.Send(conn, wireFrame{kind: websocket.BinaryFrame, data: payload})
	f := <-frames
	if f.kind != websocket.BinaryFrame || string(f.data) != string(payload) {
		t.Fatalf("binary frame changed=%+v", f)
	}
	for i := 0; i < 2; i++ {
		var frame wireFrame
		if err := frameCodec.Receive(conn, &frame); err != nil {
			t.Fatal(err)
		}
	}
	_ = conn.Close()
	select {
	case out := <-ledger.finalized:
		if !out.Charge || out.Usage.PromptTokens != 20 || out.Usage.CompletionTokens != 8 || out.Usage.CachedTokens != 3 || out.Usage.AudioInputTokens != 6 || out.Usage.AudioOutputTokens != 4 {
			t.Fatalf("usage=%+v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connection did not finalize")
	}
	if ledger.reserves.Load() != 2 {
		t.Fatal("completed turn must reserve the next hold, and duplicate usage must not rotate it")
	}
}

type failingRealtimeFinalizer struct{ *liveLedger }

func (l *failingRealtimeFinalizer) Finalize(_ context.Context, _ *gateway.Request, out gateway.Outcome) error {
	l.finalized <- out
	return errors.New("settlement unavailable")
}

func TestRealtimeFinalizationFailureStopsFurtherConsumption(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(websocket.Server{Handshake: func(*websocket.Config, *http.Request) error { return nil }, Handler: websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		for {
			var frame wireFrame
			if frameCodec.Receive(conn, &frame) != nil {
				return
			}
			calls.Add(1)
			_ = websocket.Message.Send(conn, `{"type":"response.done","response":{"id":"finalization-failure","output":[{"type":"message"}],"usage":{"input_tokens":2,"output_tokens":3}}}`)
		}
	})})
	defer upstream.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 4)}
	h, server, _ := socketFixture(t, []gateway.Target{{ChannelID: 1, CredentialID: 2, Provider: "openai", BaseURL: upstream.URL}}, ledger)
	failing := &failingRealtimeFinalizer{liveLedger: ledger}
	h.cfg.Settler = failing
	conn := dialSocket(t, server.URL, "/v1/realtime?model=gpt-realtime")
	if err := websocket.Message.Send(conn, `{"type":"response.create"}`); err != nil {
		t.Fatal(err)
	}
	var frame wireFrame
	for _, wantType := range []string{"response.done", "error"} {
		if err := frameCodec.Receive(conn, &frame); err != nil {
			t.Fatal(err)
		}
		if gjson.GetBytes(frame.data, "type").Str != wantType {
			t.Fatalf("accepted terminal or admission error lost: %s", frame.data)
		}
	}
	if gjson.GetBytes(frame.data, "status").Int() != 503 || failing.reserves.Load() != 1 || calls.Load() != 1 {
		t.Fatalf("settlement failure admitted next turn: frame=%s reserves=%d upstream=%d", frame.data, failing.reserves.Load(), calls.Load())
	}
	if out := <-failing.finalized; !out.Charge || out.Usage.CompletionTokens != 3 {
		t.Fatalf("accepted usage lost: %+v", out)
	}
	if err := frameCodec.Receive(conn, &frame); err == nil {
		t.Fatal("failed settlement left session open")
	}
	if len(failing.finalized) != 0 {
		t.Fatal("disconnect attempted duplicate finalization")
	}
}

func TestRealtimeRevocationStopsAudioInputAndSettlesAcceptedOutput(t *testing.T) {
	var forwarded atomic.Int32
	upstream := httptest.NewServer(websocket.Server{Handshake: func(*websocket.Config, *http.Request) error { return nil }, Handler: websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		var frame wireFrame
		if frameCodec.Receive(conn, &frame) != nil {
			return
		}
		forwarded.Add(1)
		_ = websocket.Message.Send(conn, `{"type":"response.output_text.delta","delta":"accepted output"}`)
		if frameCodec.Receive(conn, &frame) == nil {
			forwarded.Add(1)
		}
	})})
	defer upstream.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 2)}
	h, server, _ := socketFixture(t, []gateway.Target{{ChannelID: 1, CredentialID: 2, Provider: "openai", BaseURL: upstream.URL}}, ledger)
	auth := &changingSocketAuth{}
	h.cfg.Auth = auth
	conn := dialSocket(t, server.URL, "/v1/realtime?model=gpt-realtime")
	if err := websocket.Message.Send(conn, `{"type":"response.create"}`); err != nil {
		t.Fatal(err)
	}
	var frame wireFrame
	if err := frameCodec.Receive(conn, &frame); err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(frame.data, "delta").Str != "accepted output" {
		t.Fatalf("partial output lost: %s", frame.data)
	}
	auth.revoked.Store(true)
	if err := websocket.Message.Send(conn, `{"type":"input_audio_buffer.append","audio":"AA=="}`); err != nil {
		t.Fatal(err)
	}
	if err := frameCodec.Receive(conn, &frame); err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(frame.data, "status").Int() != 401 {
		t.Fatalf("revoked audio input accepted: %s", frame.data)
	}
	select {
	case out := <-ledger.finalized:
		if !out.Charge || !out.Delivered || !out.Usage.Estimated || out.Usage.CompletionTokens == 0 || forwarded.Load() != 1 {
			t.Fatalf("accepted work not settled or revoked frame forwarded: out=%+v forwards=%d", out, forwarded.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("revoked session did not finalize")
	}
}

func TestRealtimeRejectsMissingAuthAndUnsupportedModelBeforeDial(t *testing.T) {
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 2)}
	_, server, _ := socketFixture(t, nil, ledger)
	for _, tc := range []struct {
		path, key string
		status    int
	}{{"/v1/realtime?model=gpt", "", 401}, {"/v1/realtime", "owner", 400}, {"/v1/responses", "owner", 426}} {
		req, _ := http.NewRequest(http.MethodGet, server.URL+tc.path, nil)
		if tc.key != "" {
			req.Header.Set("Authorization", "Bearer "+tc.key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("status=%d want %d", resp.StatusCode, tc.status)
		}
	}
	if ledger.reserves.Load() != 0 {
		t.Fatal("invalid handshake reserved credits")
	}
}

func TestRealtimeEndpointAndUsageVariants(t *testing.T) {
	custom, err := realtimeConfig(gateway.Target{Provider: "azure", BaseURL: "https://example.azure.com", Settings: map[string]any{"api_version": "custom-version"}, Fingerprint: gateway.CredentialFingerprint{UserAgent: "stable-client"}, HeaderOverride: map[string]string{"X-Application": "codego"}}, "deployment")
	if err != nil || custom.Location.Query().Get("api-version") != "custom-version" || custom.Header.Get("User-Agent") != "stable-client" || custom.Header.Get("X-Application") != "codego" {
		t.Fatalf("metadata not consumed %+v %v", custom, err)
	}
	cfg, err := realtimeConfig(gateway.Target{Provider: "azure", BaseURL: "https://example.azure.com/openai/v1?api-version=my-version", Secret: "test"}, "deployment")
	if err != nil || cfg.Location.Path != "/openai/realtime" || cfg.Location.Query().Get("deployment") != "deployment" || cfg.Location.Query().Get("api-version") != "my-version" || cfg.Header.Get("Api-Key") != "test" {
		t.Fatalf("config=%+v %v", cfg, err)
	}
	if _, err := realtimeConfig(gateway.Target{Provider: "codex", BaseURL: "https://example.com"}, "gpt"); err == nil {
		t.Fatal("unsupported provider accepted")
	}
	var usage gateway.Usage
	addRealtimeUsage(&usage, gjson.Parse(`{"input_tokens":3,"output_tokens":2,"input_tokens_details":{"cached_tokens":1,"audio_tokens":2},"output_tokens_details":{"audio_tokens":1}}`))
	data, _ := json.Marshal(usage)
	if usage.CachedTokens != 1 || usage.AudioInputTokens != 2 || usage.AudioOutputTokens != 1 {
		t.Fatalf("usage=%s", data)
	}
}
