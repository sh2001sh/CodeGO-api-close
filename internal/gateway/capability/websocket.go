package capability

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

type websocketEvent struct {
	Type  string `json:"type"`
	Error *struct {
		Code    any    `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func probeWebSocket(ctx context.Context, endpoint string, input ProbeInput, base gatewayschema.CapabilityProbeState) gatewayschema.CapabilityProbeState {
	state := base
	wsURL := strings.Replace(endpoint, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+input.APIKey)
	headers.Set("OpenAI-Beta", responsesWebSocketBeta)
	headers.Set("User-Agent", "new-api-capability-probe/1")
	dialer := websocket.Dialer{HandshakeTimeout: 20 * time.Second, EnableCompression: true, Proxy: http.ProxyFromEnvironment}
	conn, response, err := dialer.DialContext(ctx, wsURL, headers)
	if response != nil {
		state.HTTPStatus = response.StatusCode
	}
	if err != nil {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		state.ErrorClass = websocketErrorClass(err)
		return state
	}
	defer conn.Close()
	state.HTTPStatus = http.StatusSwitchingProtocols
	payload, _ := platformencoding.Marshal(map[string]any{
		"type": "response.create", "model": input.Model,
		"input": "Reply with OK.", "max_output_tokens": 16,
	})
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		state.ErrorClass = websocketErrorClass(err)
		return state
	}
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	for {
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			state.Status = gatewayschema.CapabilityStatusUnsupported
			state.ErrorClass = websocketErrorClass(readErr)
			return state
		}
		var event websocketEvent
		if platformencoding.Unmarshal(raw, &event) != nil {
			continue
		}
		switch event.Type {
		case "response.completed":
			state.Status = gatewayschema.CapabilityStatusSupported
			state.ErrorClass = ""
			return state
		case "error", "response.failed", "response.incomplete":
			state.Status = gatewayschema.CapabilityStatusUnsupported
			state.ErrorClass = websocketEventErrorClass(event)
			return state
		}
	}
}

func websocketErrorClass(err error) string {
	var closeError *websocket.CloseError
	if errors.As(err, &closeError) {
		return "close_" + strconv.Itoa(closeError.Code)
	}
	return classifyTransportError(err)
}

func websocketEventErrorClass(event websocketEvent) string {
	if event.Error == nil {
		return event.Type
	}
	if value := strings.TrimSpace(event.Error.Type); value != "" {
		return sanitizeClass(value)
	}
	if value := strings.TrimSpace(event.Error.Message); value != "" {
		return messageClass(value)
	}
	return sanitizeClass(strings.TrimSpace(event.Type))
}
