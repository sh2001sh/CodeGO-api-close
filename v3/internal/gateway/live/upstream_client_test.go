package live

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/credentials"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func TestResponsesSocketConsumesClientAndConvertedOverrides(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if gjson.GetBytes(body, "temperature").Float() != 0.25 || r.Header.Get("X-Selected") != "credential-7" || r.Header.Get("X-Final-Signature") != "overridden" {
			t.Errorf("converted overrides missing body=%s header=%s", body, r.Header.Get("X-Selected"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(418)
		testResponseSSE(w, "resp_overrides")
	}))
	defer upstream.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 1)}
	target := gateway.Target{ChannelID: 1, CredentialID: 7, Provider: "openai", BaseURL: upstream.URL,
		ParamOverride: map[string]any{"temperature": 0.25}, HeaderOverride: map[string]string{"X-Selected": "credential-7"}, StatusCodeMapping: map[string]int{"418": 200}}
	h, server, _ := socketFixture(t, []gateway.Target{target}, ledger)
	h.cfg.Providers["openai"] = h.TrackingProvider(finalizedResponses{})
	client := &http.Client{Transport: http.DefaultTransport}
	var calls int
	h.cfg.Clients = func(_ context.Context, selected gateway.Target) (*http.Client, error) {
		calls++
		if selected.CredentialID != 7 || selected.ChannelID != 1 {
			t.Errorf("wrong client target %+v", selected)
		}
		return client, nil
	}
	conn := dialSocket(t, server.URL, "/responses")
	_ = websocket.Message.Send(conn, `{"type":"response.create","model":"gpt-test","temperature":1,"input":"test"}`)
	readSocketCompleted(t, conn)
	out := <-ledger.finalized
	if calls != 1 || !out.Charge || out.Terminal != gateway.TerminalCompleted || client.CheckRedirect != nil {
		t.Fatalf("client/mapped outcome calls=%d out=%+v", calls, out)
	}
}

type finalizedResponses struct{ responses.Provider }

func (finalizedResponses) FinalizeRequest(_ context.Context, out *http.Request, req *gateway.Request, _ gateway.Target) error {
	copy, err := out.GetBody()
	if err != nil {
		return err
	}
	defer func() { _ = copy.Close() }()
	data, err := io.ReadAll(copy)
	if err != nil {
		return err
	}
	if gjson.GetBytes(data, "temperature").Float() != 0.25 || gjson.GetBytes(req.Body, "temperature").Float() != 1 {
		return errors.New("signer did not see overridden bytes and frozen original input")
	}
	out.Header.Set("X-Final-Signature", "overridden")
	return nil
}

func TestRealtimeFrameOverridesKeepFrozenBillingInput(t *testing.T) {
	request := &gateway.Request{Model: "alias", Body: []byte(`{"model":"alias"}`)}
	target := gateway.Target{ParamOverride: map[string]any{"operations": []any{map[string]any{"mode": "set", "path": "response.temperature", "value": 0.25}}}}
	data, err := realtimeFrameRequest(context.Background(), request, target, []byte(`{"type":"response.create","response":{"model":"mapped","temperature":1}}`))
	if err != nil || gjson.GetBytes(data, "response.temperature").Float() != 0.25 || string(request.Body) != `{"model":"alias"}` {
		t.Fatalf("frame=%s err=%v original=%s", data, err, request.Body)
	}
	target.ParamOverride = map[string]any{"operations": "invalid"}
	if _, err := realtimeFrameRequest(context.Background(), request, target, []byte(`{"type":"response.create"}`)); err == nil {
		t.Fatal("invalid realtime override was silently forwarded")
	}
}

func TestRealtimeConfiguredTLSIdentityUsesDuplexUpgrade(t *testing.T) {
	upstream := httptest.NewTLSServer(websocket.Server{Handshake: func(_ *websocket.Config, r *http.Request) error {
		if r.Header.Get("User-Agent") != "stable-realtime" || r.Header.Get("X-Identity") != "selected" {
			t.Errorf("credential identity missing: %+v", r.Header)
		}
		return nil
	}, Handler: websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		var frame wireFrame
		if err := frameCodec.Receive(conn, &frame); err != nil {
			return
		}
		_ = frameCodec.Send(conn, frame)
		_ = websocket.Message.Send(conn, `{"type":"response.done","response":{"id":"resp_tls","output":[{}],"usage":{"input_tokens":3,"output_tokens":2}}}`)
		_ = frameCodec.Receive(conn, &frame)
	})})
	defer upstream.Close()
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	pool := credentials.NewTransportPool(credentials.TransportConfig{RootCAs: roots})
	defer pool.CloseIdle()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 1)}
	target := gateway.Target{ChannelID: 1, CredentialID: 7, Provider: "openai", BaseURL: upstream.URL,
		Fingerprint: gateway.CredentialFingerprint{TLSProfile: "firefox", UserAgent: "stable-realtime"}, HeaderOverride: map[string]string{"X-Identity": "selected"}}
	h, server, _ := socketFixture(t, []gateway.Target{target}, ledger)
	h.cfg.Clients = func(_ context.Context, selected gateway.Target) (*http.Client, error) {
		if selected.CredentialID != 7 || selected.Fingerprint.TLSProfile != "firefox" {
			t.Errorf("wrong selected identity %+v", selected)
		}
		client, _, err := pool.Client(selected.CredentialID, selected.ProxyURL, credentials.Fingerprint{TLSProfile: selected.Fingerprint.TLSProfile, UserAgent: selected.Fingerprint.UserAgent})
		return client, err
	}
	conn := dialSocket(t, server.URL, "/v1/realtime?model=gpt-realtime")
	_ = frameCodec.Send(conn, wireFrame{kind: websocket.BinaryFrame, data: []byte{0, 1, 255}})
	var echo, done wireFrame
	if err := frameCodec.Receive(conn, &echo); err != nil || echo.kind != websocket.BinaryFrame || string(echo.data) != string([]byte{0, 1, 255}) {
		t.Fatalf("duplex echo=%+v err=%v", echo, err)
	}
	if err := frameCodec.Receive(conn, &done); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	select {
	case out := <-ledger.finalized:
		if !out.Charge || out.Usage.PromptTokens != 3 || out.Usage.CompletionTokens != 2 {
			t.Fatalf("usage=%+v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("configured connection did not finalize")
	}
}

func TestConfiguredLiveClientFailureCannotFallback(t *testing.T) {
	h, _, _ := socketFixture(t, nil, &liveLedger{})
	for _, fn := range []gateway.ClientProvider{
		func(context.Context, gateway.Target) (*http.Client, error) { return nil, errors.New("unavailable") },
		func(context.Context, gateway.Target) (*http.Client, error) { return nil, nil },
	} {
		h.cfg.Clients = fn
		if _, err := h.upstreamClient(context.Background(), gateway.Target{}); err == nil {
			t.Fatal("configured client failure silently used a default transport")
		}
	}
}
