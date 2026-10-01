package providers_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func sparkReplayFixture() replayFixture {
	return replayFixture{id: "xunfei", name: "xunfei", model: "SparkDesk-v3.5", secret: "test-app|test-secret|test-key", websocket: true}
}

func replayWebsocketServer(t *testing.T, h *replayHarness, f replayFixture, mode string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		h.calls.Add(1)
		request := conn.Request()
		query := request.URL.Query()
		canonical := "host: " + query.Get("host") + "\ndate: " + query.Get("date") + "\nGET " + request.URL.EscapedPath() + " HTTP/1.1"
		mac := hmac.New(sha256.New, []byte("test-secret"))
		_, _ = mac.Write([]byte(canonical))
		auth, err := base64.StdEncoding.DecodeString(query.Get("authorization"))
		if err != nil || !strings.Contains(string(auth), `username="test-key"`) || !strings.Contains(string(auth), `signature="`+base64.StdEncoding.EncodeToString(mac.Sum(nil))+`"`) {
			t.Error("Spark native WebSocket auth signature invalid")
		}
		if request.URL.Path != "/v3.5/chat" {
			t.Errorf("Spark native endpoint=%s", request.URL.Path)
		}
		var body []byte
		if err := websocket.Message.Receive(conn, &body); err != nil {
			t.Error(err)
			return
		}
		assertJSON(t, body, "header.app_id", "test-app")
		assertJSON(t, body, "payload.message.text.0.content", "hello")
		assertJSON(t, body, "parameter.chat.domain", "generalv3.5")
		forbidJSON(t, body, "model", "messages", "stream_options")
		send := func(status, seq int, text string, usage bool) {
			payload := map[string]any{"choices": map[string]any{"status": status, "seq": seq, "text": []any{map[string]any{"role": "assistant", "content": text, "index": 0}}}}
			if usage {
				payload["usage"] = map[string]any{"text": map[string]int{"prompt_tokens": 10, "completion_tokens": 3, "total_tokens": 13}}
			}
			data, _ := json.Marshal(map[string]any{"header": map[string]any{"code": 0, "sid": "s1"}, "payload": payload})
			_ = websocket.Message.Send(conn, string(data))
		}
		if mode == "rate_limited" {
			_ = websocket.Message.Send(conn, `{"header":{"code":10013,"message":"rate limited"}}`)
			return
		}
		if mode == "empty" {
			send(2, 0, "", false)
			return
		}
		send(0, 0, "hello", false)
		switch mode {
		case "truncated_after_output":
			return
		case "timeout_after_output":
			_ = websocket.Message.Receive(conn, &body)
			return
		case "client_canceled":
			select {
			case <-h.clientLeft:
			case <-time.After(2 * time.Second):
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		send(2, 1, "", mode == "completed" || mode == "client_canceled")
	}))
}
