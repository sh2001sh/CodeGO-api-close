package capability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	"github.com/stretchr/testify/require"
)

func TestProbeWebSocketSeparatesHandshake401FromUpstreamClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	state := probeWebSocket(context.Background(), server.URL, ProbeInput{APIKey: "bad-key", Model: "gpt-5"}, gatewayschema.CapabilityProbeState{})
	require.Equal(t, gatewayschema.CapabilityStatusError, state.Status)
	require.Equal(t, http.StatusUnauthorized, state.HTTPStatus)
	require.Equal(t, "http_401", state.ErrorClass)
}

func TestProbeWebSocketRecordsCloseAfterSuccessfulUpgrade(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.Header.Get("OpenAI-Beta"), "responses_websockets=") {
			t.Errorf("missing responses websocket beta header")
			return
		}
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		_, _, err = conn.ReadMessage()
		if err != nil {
			t.Errorf("read probe message: %v", err)
			return
		}
		if err := conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "busy"),
			time.Now().Add(time.Second)); err != nil {
			t.Errorf("write close frame: %v", err)
		}
	}))
	defer server.Close()

	state := probeWebSocket(context.Background(), server.URL, ProbeInput{APIKey: "key", Model: "gpt-5"}, gatewayschema.CapabilityProbeState{})
	require.Equal(t, gatewayschema.CapabilityStatusError, state.Status)
	require.Equal(t, http.StatusSwitchingProtocols, state.HTTPStatus)
	require.Equal(t, "close_1013", state.ErrorClass)
}

func TestWebSocketEventErrorClassReadsNestedResponseError(t *testing.T) {
	event := websocketEvent{Type: "response.failed", Response: &struct {
		Error *websocketEventError `json:"error"`
	}{Error: &websocketEventError{Type: "authentication_error", Message: "invalid token"}}}
	require.Equal(t, "authentication_error", websocketEventErrorClass(event))
	require.True(t, strings.Contains(event.Response.Error.Message, "token"))
}
