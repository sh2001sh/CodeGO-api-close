package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bridge"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/xunfei"
	"golang.org/x/net/websocket"
)

func TestSparkNativeTransportUsesClientAPIVersion(t *testing.T) {
	captured := make(chan string, 1)
	upstream := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		captured <- conn.Request().URL.Path
		var request []byte
		if err := websocket.Message.Receive(conn, &request); err != nil {
			t.Error(err)
			return
		}
		frame := `{"header":{"code":0,"status":2,"sid":"fixture"},"payload":{"choices":{"status":2,"seq":0,"text":[{"role":"assistant","content":"hello"}]},"usage":{"text":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}}}`
		if err := websocket.Message.Send(conn, frame); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(upstream.Close)
	h := fixtureGateway(t, upstream.URL, gateway.Target{Provider: xunfei.ID, UpstreamModel: "SparkDesk", Secret: "test-app|test-secret|test-key"}, bridge.Provider{Chat: xunfei.Provider{}})
	r, _ := http.NewRequest(http.MethodPost, h.gw.URL+"/v1/chat/completions?api-version=v3.5", strings.NewReader(streamBody))
	r.Header.Set("Authorization", "Bearer sk-test")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	out := h.outcome()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "hello") || out.Terminal != gateway.TerminalCompleted || out.Usage.CompletionTokens != 2 {
		t.Fatalf("status=%d body=%s outcome=%+v", resp.StatusCode, body, out)
	}
	if path := <-captured; path != "/v3.5/chat" {
		t.Fatalf("Spark api-version ignored: %s", path)
	}
}

func TestSparkRejectsInvalidClientAPIVersionBeforeNetwork(t *testing.T) {
	h := fixtureGateway(t, "http://127.0.0.1:1", gateway.Target{Provider: xunfei.ID, UpstreamModel: "SparkDesk", Secret: "test-app|test-secret|test-key"}, bridge.Provider{Chat: xunfei.Provider{}})
	r, _ := http.NewRequest(http.MethodPost, h.gw.URL+"/v1/chat/completions?api-version=invalid", strings.NewReader(streamBody))
	r.Header.Set("Authorization", "Bearer sk-test")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	out := h.outcome()
	if resp.StatusCode != 400 || !strings.Contains(string(body), "invalid_api_version") || out.Charge || len(h.planner.results()) != 1 {
		t.Fatalf("status=%d body=%s outcome=%+v", resp.StatusCode, body, out)
	}
}
