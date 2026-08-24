package capability

import (
	"context"
	"encoding/json"
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

func TestProbeWebSocketUsesStandardResponsesCreatePayload(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Sec-WebSocket-Extensions") != "" {
			t.Errorf("probe must not negotiate permessage-deflate: %q", request.Header.Get("Sec-WebSocket-Extensions"))
		}
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read probe message: %v", err)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Errorf("decode probe message: %v", err)
			return
		}
		if _, exists := payload["stream_id"]; exists || payload["store"] != false || payload["input"] == nil {
			t.Errorf("non-standard probe payload: %s", raw)
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed"}`))
	}))
	defer server.Close()

	state := probeWebSocket(context.Background(), server.URL, ProbeInput{APIKey: "key", Model: "gpt-5"}, gatewayschema.CapabilityProbeState{})
	require.Equal(t, gatewayschema.CapabilityStatusSupported, state.Status)
	require.Equal(t, gatewayschema.CapabilityStatusSupported, state.HandshakeStatus)
	require.Equal(t, gatewayschema.CapabilityStatusSupported, state.RequestStatus)
}

func TestProbeWebSocketUsesCodexCompatiblePayloadWithoutStreamID(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read probe message: %v", err)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Errorf("decode probe message: %v", err)
			return
		}
		if _, exists := payload["stream_id"]; exists {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"Unsupported parameter: stream_id"}}`))
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed"}`))
	}))
	defer server.Close()

	state := probeWebSocket(context.Background(), server.URL, ProbeInput{APIKey: "key", Model: "gpt-5.4-mini"}, gatewayschema.CapabilityProbeState{})
	require.Equal(t, gatewayschema.CapabilityStatusSupported, state.Status)
	require.Equal(t, gatewayschema.CapabilityStatusSupported, state.HandshakeStatus)
	require.Equal(t, gatewayschema.CapabilityStatusSupported, state.RequestStatus)
}

func TestWebSocket426IsTransientError(t *testing.T) {
	state := gatewayschema.CapabilityProbeState{
		Status: gatewayschema.CapabilityStatusError, HTTPStatus: http.StatusUpgradeRequired, ErrorClass: "http_426",
	}
	require.Equal(t, gatewayschema.CapabilityStatusError, websocketFailureStatus(state.ErrorClass))
	require.True(t, IsTransientProbeFailure(state))
}

func TestWebSocketEventErrorClassReadsNestedResponseError(t *testing.T) {
	event := websocketEvent{Type: "response.failed", Response: &struct {
		Error *websocketEventError `json:"error"`
	}{Error: &websocketEventError{Type: "authentication_error", Message: "invalid token"}}}
	require.Equal(t, "authentication_error", websocketEventErrorClass(event))
	require.True(t, strings.Contains(event.Response.Error.Message, "token"))
}
