package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func dialResponsesWebsocketTest(t *testing.T, execute func(context.Context, *responsesWebsocketHTTPWriter, []byte)) *websocket.Conn {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := responsesWebsocketUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		serveResponsesWebsocket(conn, ctx, cancel, execute)
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	return conn
}

func readResponsesWebsocketTest(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	_, payload, err := conn.ReadMessage()
	require.NoError(t, err)
	return string(payload)
}

func TestResponsesWebsocketInterruptCancelsHTTPUpstreamAndReusesConnection(t *testing.T) {
	for _, eventType := range []string{"response.interrupt", "response.cancel"} {
		t.Run(eventType, func(t *testing.T) {
			upstreamCancelled := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"id\":\"resp_active\"}}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(upstreamCancelled)
			}))
			t.Cleanup(upstream.Close)
			var turns atomic.Int32
			conn := dialResponsesWebsocketTest(t, func(ctx context.Context, writer *responsesWebsocketHTTPWriter, _ []byte) {
				if turns.Add(1) > 1 {
					writer.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_next\",\"output\":[]}}\n\n")
					return
				}
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, upstream.URL, nil)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return
				}
				defer resp.Body.Close()
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.Copy(writer, resp.Body)
				// A late upstream completion must not escape after interruption.
				_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\"}\n\n")
			})
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"test","input":"hello"}`)))
			require.Contains(t, readResponsesWebsocketTest(t, conn), "response.created")
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancel","response_id":"resp_stale"}`)))
			require.Contains(t, readResponsesWebsocketTest(t, conn), "response_not_found")
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":%q,"response_id":"resp_active"}`, eventType))))
			require.Contains(t, readResponsesWebsocketTest(t, conn), `"type":"response.cancelled"`)
			select {
			case <-upstreamCancelled:
			case <-time.After(time.Second):
				t.Fatal("HTTP upstream did not stop after interruption")
			}
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"test","input":"next"}`)))
			require.Contains(t, readResponsesWebsocketTest(t, conn), "resp_next")
			require.EqualValues(t, 2, turns.Load())
		})
	}
}

func TestResponsesWebsocketIdleInterruptAndInvalidFramesDoNotExecute(t *testing.T) {
	var turns atomic.Int32
	conn := dialResponsesWebsocketTest(t, func(context.Context, *responsesWebsocketHTTPWriter, []byte) { turns.Add(1) })
	for _, payload := range []string{`{"type":"response.interrupt"}`, `{"type":"response.cancel"}`} {
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(payload)))
	}
	for _, payload := range []string{`{`, `{"type":"response.unknown"}`, `{"type":"response.interrupt","response_id":"missing"}`} {
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(payload)))
		require.Contains(t, readResponsesWebsocketTest(t, conn), `"type":"error"`)
	}
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"test","generate":false}`)))
	require.Contains(t, readResponsesWebsocketTest(t, conn), "response.created")
	require.Contains(t, readResponsesWebsocketTest(t, conn), "response.completed")
	require.Zero(t, turns.Load())
}

func TestResponsesWebsocketDisconnectCancelsActiveTurn(t *testing.T) {
	cancelled := make(chan struct{})
	conn := dialResponsesWebsocketTest(t, func(ctx context.Context, writer *responsesWebsocketHTTPWriter, _ []byte) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n")
		<-ctx.Done()
		close(cancelled)
	})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"test"}`)))
	require.Contains(t, readResponsesWebsocketTest(t, conn), "response.created")
	require.NoError(t, conn.Close())
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not cancel active turn")
	}
}

func TestResponsesWebsocketInterruptedOutputIsNeverCached(t *testing.T) {
	writer := newResponsesWebsocketHTTPWriter(nil, func() {})
	writer.observePayloadLocked([]byte(`{"type":"response.created","response":{"id":"resp_1"}}`))
	require.True(t, writer.Interrupt("resp_1"))
	writer.observePayloadLocked([]byte(`{"type":"response.completed","response":{"id":"resp_1","output":[]}}`))
	_, _, completed := writer.Completion()
	require.False(t, completed)
}
