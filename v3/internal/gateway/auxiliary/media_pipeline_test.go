package auxiliary

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"golang.org/x/net/websocket"
)

// Route the real default native WebSocket adapter into the local provider.
type localVolcMediaAdapter struct{}

func (localVolcMediaAdapter) Build(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	native := target
	native.BaseURL = ""
	r, err := buildVolcMedia(ctx, req, native, in)
	if err != nil {
		return nil, err
	}
	local, err := url.Parse(target.BaseURL)
	if err != nil {
		return nil, err
	}
	r.URL.Scheme, r.URL.Host = "ws", local.Host
	if local.Scheme == "https" {
		r.URL.Scheme = "wss"
	}
	return r, nil
}

func (localVolcMediaAdapter) Decode(ctx context.Context, req *gateway.Request, target gateway.Target, in Input, resp *http.Response) (Response, error) {
	return (&mediaAdapter{provider: "volcengine"}).Decode(ctx, req, target, in, resp)
}

func TestVolcMediaGatewayRetriesBeforeAudioAndSettlesOnce(t *testing.T) {
	var calls atomic.Int32
	broken := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		var data []byte
		_ = websocket.Message.Receive(conn, &data)
		calls.Add(1)
		_ = websocket.Message.Send(conn, volcMediaAudioFrame(1, []byte{99}))
	}))
	defer broken.Close()
	success := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		var data []byte
		_ = websocket.Message.Receive(conn, &data)
		calls.Add(1)
		_ = websocket.Message.Send(conn, volcMediaAudioFrame(-1, []byte{0, 1, 2, 255}))
	}))
	defer success.Close()
	h, plan, settle, limits := testHandler(t, broken.URL, success.URL)
	for i := range plan.targets {
		plan.targets[i].Provider = "volcengine"
		plan.targets[i].Secret = "app|native-token"
	}
	h.adapters["volcengine"] = localVolcMediaAdapter{}
	w := invoke(h, "/v1/audio/speech", `{"model":"alias","input":"Hello","voice":"alloy","response_format":"wav"}`)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), []byte{0, 1, 2, 255}) || calls.Load() != 2 || limits.released != 2 {
		t.Fatalf("code=%d body=%v calls=%d leases=%d", w.Code, w.Body.Bytes(), calls.Load(), limits.released)
	}
	if !settle.finalized || settle.reserves != 1 || !settle.out.Charge || !settle.out.Delivered || !settle.out.Usage.Estimated {
		t.Fatalf("settlement %+v", settle)
	}
	if len(settle.req.Attempts) != 2 || settle.req.Attempts[0].Result.OK || !settle.req.Attempts[1].Result.OK {
		t.Fatalf("attempts %+v", settle.req.Attempts)
	}
	if w.Header().Get("Authorization") != "" || w.Header().Get("X-Codego-Audio-Characters") != "" {
		t.Error("private upstream/accounting headers exposed")
	}
}

func TestMediaVersionedBaseAndExactLargeSeed(t *testing.T) {
	req := &gateway.Request{Model: "image", Body: []byte(`{"model":"image","prompt":"cat","extra_fields":{"seed":9223372036854775807}}`)}
	r, err := mediaAdapters()["minimax"].Build(context.Background(), req, gateway.Target{BaseURL: "https://example.com/gateway/v1", Secret: "secret"}, Input{Operation: Images})
	if err != nil {
		t.Fatal(err)
	}
	if r.URL.Path != "/gateway/v1/image_generation" {
		t.Fatalf("duplicated version %s", r.URL)
	}
	_ = r.Body.Close()
	r, err = mediaAdapters()["jimeng"].Build(context.Background(), req, gateway.Target{BaseURL: "https://example.com", Secret: "ak|sk"}, Input{Operation: Images})
	if err != nil {
		t.Fatal(err)
	}
	body, err := r.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	var data bytes.Buffer
	_, _ = data.ReadFrom(body)
	if !bytes.Contains(data.Bytes(), []byte(`"seed":9223372036854775807`)) {
		t.Fatalf("image seed lost precision %s", data.Bytes())
	}
}
