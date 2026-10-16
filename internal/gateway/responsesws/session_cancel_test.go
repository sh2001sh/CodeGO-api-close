package responsesws

import (
	"context"
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

func TestSessionCancellationUnblocksSilentUpstreamAndAllowsNextTurn(t *testing.T) {
	var connections atomic.Int32
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		n := connections.Add(1)
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		if n == 1 {
			_, _, _ = conn.ReadMessage()
			close(closed)
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_next","output":[]}}`))
	}))
	defer server.Close()
	session := NewSession()
	defer session.Close()
	require.NoError(t, session.BindRoute(10, 0, true))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, err := session.Do(ctx, server.URL, http.Header{}, strings.NewReader(`{"model":"test"}`))
	require.NoError(t, err)
	cancel()
	readDone := make(chan error, 1)
	go func() { _, err := io.ReadAll(resp.Body); readDone <- err }()
	select {
	case err := <-readDone:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("cancellation left the response reader blocked")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cancelled native connection remained open")
	}
	// The new turn reconnects without retrying the interrupted request.
	next, err := session.Do(context.Background(), server.URL, http.Header{}, strings.NewReader(`{"model":"test"}`))
	require.NoError(t, err)
	body, err := io.ReadAll(next.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "resp_next")
	require.EqualValues(t, 2, connections.Load())
}

func TestTerminalWebsocketCancellationAliases(t *testing.T) {
	for _, event := range []string{"response.cancelled", "response.canceled", "response.incomplete"} {
		require.True(t, terminalWebSocketEvent([]byte(`{"type":"`+event+`"}`)))
	}
	require.False(t, terminalWebSocketEvent([]byte(`{"type":"response.output_text.delta"}`)))
}
