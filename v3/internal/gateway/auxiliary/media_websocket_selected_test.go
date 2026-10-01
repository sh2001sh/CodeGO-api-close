package auxiliary

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func TestVolcMediaSelectedTransportPreservesNativeUpgradeAuthAndFrames(t *testing.T) {
	var wireCalls, frames, selections atomic.Int64
	server := httptest.NewServer(websocket.Server{Handshake: func(_ *websocket.Config, r *http.Request) error {
		if r.URL.Path != "/api/v1/tts/ws_binary" || r.Header.Get("Authorization") != "Bearer;native-token" || r.Header.Get("X-Configured") != "retained" || r.Header.Get("User-Agent") != "stable-credential" {
			t.Error("selected native handshake lost final headers")
		}
		return nil
	}, Handler: func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		var frame []byte
		if err := websocket.Message.Receive(conn, &frame); err != nil {
			t.Error(err)
			return
		}
		frames.Add(1)
		if len(frame) < 8 || gjson.GetBytes(frame[8:], "request.text").String() != "Hello" || gjson.GetBytes(frame[8:], "request.operation").String() != "submit" {
			t.Errorf("native request frame=%v", frame)
		}
		_ = websocket.Message.Send(conn, volcMediaAudioFrame(-1, []byte{0, 1, 2, 255}))
	}})
	defer server.Close()
	h, plan, settle, limits := testHandler(t, server.URL)
	plan.targets[0].Provider, plan.targets[0].Secret, plan.targets[0].CredentialID = "volcengine", "app|native-token", 22
	plan.targets[0].ProxyURL = "http://must-not-dial.invalid:8080"
	plan.targets[0].Fingerprint = gateway.CredentialFingerprint{TLSProfile: "configured", UserAgent: "stable-credential"}
	plan.targets[0].HeaderOverride = map[string]string{"X-Configured": "retained"}
	original := plan.targets[0]
	h.adapters["volcengine"] = localVolcMediaAdapter{}
	selected := &http.Client{Transport: vectorRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		wireCalls.Add(1)
		if r.Method != http.MethodGet || r.URL.Scheme != "http" || r.RequestURI != "" || r.Body != nil || r.GetBody != nil || r.ContentLength != 0 || r.Header.Get("Content-Length") != "" || r.Header.Get("Sec-WebSocket-Key") == "" {
			t.Error("native POST payload leaked into handshake")
		}
		clone := r.Clone(r.Context())
		clone.Header.Set("User-Agent", "stable-credential")
		return server.Client().Transport.RoundTrip(clone)
	})}
	h.cfg.Clients = func(_ context.Context, candidate gateway.Target) (*http.Client, error) {
		selections.Add(1)
		if !reflect.DeepEqual(candidate, original) {
			t.Error("selected credential identity lost")
		}
		return selected, nil
	}
	w := invoke(h, "/v1/audio/speech", `{"model":"alias","input":"Hello","voice":"alloy","response_format":"pcm"}`)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), []byte{0, 1, 2, 255}) || wireCalls.Load() != 1 || frames.Load() != 1 || selections.Load() != 1 || !settle.out.Charge || limits.released != 1 || selected.CheckRedirect != nil {
		t.Fatalf("status=%d wire=%d frames=%d selections=%d outcome=%+v", w.Code, wireCalls.Load(), frames.Load(), selections.Load(), settle.out)
	}
}

func TestVolcMediaSelectedFailureAndPolicyCannotFallbackToDirectDial(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	for _, deny := range []bool{false, true} {
		h, plan, settle, limits := testHandler(t, server.URL)
		plan.targets[0].Provider, plan.targets[0].Secret = "volcengine", "app|native-token"
		h.adapters["volcengine"] = localVolcMediaAdapter{}
		var selectedCalls atomic.Int64
		h.cfg.Clients = func(context.Context, gateway.Target) (*http.Client, error) {
			return &http.Client{Transport: vectorRoundTripFunc(func(*http.Request) (*http.Response, error) {
				selectedCalls.Add(1)
				return nil, errors.New("private selected-client failure")
			})}, nil
		}
		if deny {
			h.cfg.TargetPolicy = func(*gateway.Request, gateway.Target) error {
				return &gateway.UpstreamError{Status: 403, Code: "policy_denied", Type: "permission_error", Message: "request denied"}
			}
		}
		w := invoke(h, "/v1/audio/speech", `{"model":"alias","input":"Hello","voice":"alloy"}`)
		want, status := int64(1), 502
		if deny {
			want, status = 0, 403
		}
		if w.Code != status || calls.Load() != 0 || selectedCalls.Load() != want || settle.out.Charge || limits.acquired != limits.released || strings.Contains(w.Body.String(), "private selected-client failure") {
			t.Fatalf("deny=%v status=%d selected=%d wire=%d outcome=%+v", deny, w.Code, selectedCalls.Load(), calls.Load(), settle.out)
		}
		if deny && settle.reserves != 0 {
			t.Fatal("policy refusal reserved credits")
		}
	}
}

func TestVolcMediaActualMarketClientRejectsPrivateNativeURLBeforeDial(t *testing.T) {
	var calls, dials atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	defer transport.CloseIdleConnections()
	market, err := httpx.MarketClient(&http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	h, plan, settle, limits := testHandler(t, server.URL)
	plan.targets[0].Provider, plan.targets[0].Secret, plan.targets[0].Scope = "volcengine", "app|native-token", "marketplace"
	h.adapters["volcengine"] = localVolcMediaAdapter{}
	h.cfg.Clients = func(context.Context, gateway.Target) (*http.Client, error) { return market, nil }
	w := invoke(h, "/v1/audio/speech", `{"model":"alias","input":"Hello","voice":"alloy"}`)
	if w.Code != 502 || dials.Load() != 0 || calls.Load() != 0 || settle.out.Charge || !settle.finalized || limits.released != 1 {
		t.Fatalf("status=%d dials=%d calls=%d outcome=%+v", w.Code, dials.Load(), calls.Load(), settle.out)
	}
}

func TestVolcMediaMissingSelectedContextFailsWithoutNetwork(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	r, _ := localVolcMediaRequest(t, context.Background(), server.URL)
	_, err := mediaWebsocketRoundTrip(context.Background(), r)
	if err == nil || calls.Load() != 0 {
		t.Fatalf("missing selector fell back err=%v calls=%d", err, calls.Load())
	}
	_ = r.Body.Close()
}

var _ io.ReadWriteCloser = (*mediaUpgrade)(nil)
