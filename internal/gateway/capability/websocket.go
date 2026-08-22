package capability

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

type websocketEvent struct {
	Type     string               `json:"type"`
	Error    *websocketEventError `json:"error"`
	Response *struct {
		Error *websocketEventError `json:"error"`
	} `json:"response"`
}

type websocketEventError struct {
	Code    any    `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

const websocketProbeTimeout = 20 * time.Second

func probeWebSocket(ctx context.Context, endpoint string, input ProbeInput, base gatewayschema.CapabilityProbeState) gatewayschema.CapabilityProbeState {
	return probeWebSocketCandidate(ctx, nil, endpoint, input, base)
}

func probeWebSocketCandidate(ctx context.Context, _ *http.Client, endpoint string, input ProbeInput, base gatewayschema.CapabilityProbeState) gatewayschema.CapabilityProbeState {
	probeCtx, cancel := context.WithTimeout(ctx, websocketProbeTimeout)
	defer cancel()
	state := base
	state.HandshakeStatus = gatewayschema.CapabilityStatusPending
	state.RequestStatus = gatewayschema.CapabilityStatusPending
	wsURL := strings.Replace(endpoint, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	headers := probeHeaders(input, nil)
	headers.Set("Authorization", "Bearer "+input.APIKey)
	webSocketBeta := responsesWebSocketBeta
	if existing := strings.TrimSpace(headers.Get("OpenAI-Beta")); existing != "" {
		webSocketBeta = existing + "," + responsesWebSocketBeta
	}
	headers.Set("OpenAI-Beta", webSocketBeta)
	headers.Set("User-Agent", "new-api-capability-probe/1")
	// Do not negotiate permessage-deflate during probing. Several compatible
	// gateways advertise it but fail while reading a compressed first frame.
	dialer := websocket.Dialer{HandshakeTimeout: 20 * time.Second, EnableCompression: false, Proxy: http.ProxyFromEnvironment}
	conn, response, err := dialer.DialContext(probeCtx, wsURL, headers)
	if response != nil {
		state.HTTPStatus = response.StatusCode
	}
	if err != nil {
		state.HandshakeStatus = gatewayschema.CapabilityStatusError
		state.RequestStatus = gatewayschema.CapabilityStatusUnknown
		state.ErrorClass = websocketDialErrorClass(err, response)
		if class := websocketHandshakeResponseClass(response); class != "" {
			state.ErrorClass = class
		}
		state.Status = websocketFailureStatus(state.ErrorClass)
		if state.Status == gatewayschema.CapabilityStatusUnsupported {
			state.HandshakeStatus = gatewayschema.CapabilityStatusUnsupported
		}
		return state
	}
	defer conn.Close()
	state.HTTPStatus = http.StatusSwitchingProtocols
	state.HandshakeStatus = gatewayschema.CapabilityStatusSupported
	payload, _ := platformencoding.Marshal(websocketProbeRequest(input.Model))
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		state.ErrorClass = websocketErrorClass(err)
		state.Status = websocketFailureStatus(state.ErrorClass)
		state.RequestStatus = state.Status
		return state
	}
	_ = conn.SetReadDeadline(time.Now().Add(websocketProbeTimeout))
	for {
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			state.ErrorClass = websocketErrorClass(readErr)
			state.Status = websocketFailureStatus(state.ErrorClass)
			state.RequestStatus = state.Status
			return state
		}
		var event websocketEvent
		if platformencoding.Unmarshal(raw, &event) != nil {
			continue
		}
		switch event.Type {
		case "response.completed", "response.done":
			state.Status = gatewayschema.CapabilityStatusSupported
			state.RequestStatus = gatewayschema.CapabilityStatusSupported
			state.ErrorClass = ""
			return state
		case "error", "response.failed", "response.incomplete":
			state.ErrorClass = websocketEventErrorClass(event)
			state.Status = websocketFailureStatus(state.ErrorClass)
			state.RequestStatus = state.Status
			return state
		}
	}
}

func websocketProbeRequest(model string) map[string]any {
	return map[string]any{
		"type":      "response.create",
		"stream_id": "capability-probe",
		"model":     strings.TrimSpace(model),
		"store":     false,
		"input": []any{map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{map[string]any{
				"type": "input_text",
				"text": "Reply with OK.",
			}},
		}},
		"tools": []any{},
	}
}

func websocketFailureStatus(errorClass string) string {
	class := strings.ToLower(strings.TrimSpace(errorClass))
	if strings.HasPrefix(class, "http_") {
		status, err := strconv.Atoi(strings.TrimPrefix(class, "http_"))
		if err == nil {
			switch status {
			case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
				return gatewayschema.CapabilityStatusUnsupported
			}
		}
	}
	switch class {
	case "unsupported", "unsupported_operation", "protocol_error", "close_1002", "close_1003":
		return gatewayschema.CapabilityStatusUnsupported
	default:
		return gatewayschema.CapabilityStatusError
	}
}

// IsTransientProbeFailure identifies errors that should be probed again instead
// of being cached as a definitive transport capability result.
func IsTransientProbeFailure(state gatewayschema.CapabilityProbeState) bool {
	if state.Status != gatewayschema.CapabilityStatusError {
		return false
	}
	if state.HTTPStatus == http.StatusUpgradeRequired || state.HTTPStatus == http.StatusTooManyRequests || state.HTTPStatus >= http.StatusInternalServerError {
		return true
	}
	class := strings.ToLower(strings.TrimSpace(state.ErrorClass))
	if strings.HasPrefix(class, "http_5") || strings.HasPrefix(class, "http_426") || strings.HasPrefix(class, "http_429") {
		return true
	}
	for _, marker := range []string{
		"close_1001", "close_1006", "close_1011", "close_1012", "close_1013",
		"timeout", "transport", "unexpected_eof", "connection_reset", "handshake",
		"rate_limit", "overloaded", "upstream", "background_", "compaction_",
	} {
		if strings.Contains(class, marker) {
			return true
		}
	}
	return false
}

func websocketHandshakeResponseClass(response *http.Response) string {
	if response == nil || response.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil || len(raw) == 0 {
		return ""
	}
	class := responseErrorClass(raw, response.StatusCode)
	if strings.HasPrefix(class, "http_") {
		return ""
	}
	return class
}

func websocketErrorClass(err error) string {
	var closeError *websocket.CloseError
	if errors.As(err, &closeError) {
		return "close_" + strconv.Itoa(closeError.Code)
	}
	return classifyTransportError(err)
}

func websocketDialErrorClass(err error, response *http.Response) string {
	if response != nil && response.StatusCode > 0 {
		return "http_" + strconv.Itoa(response.StatusCode)
	}
	return websocketErrorClass(err)
}

func websocketEventErrorClass(event websocketEvent) string {
	eventError := event.Error
	if eventError == nil && event.Response != nil {
		eventError = event.Response.Error
	}
	if eventError == nil {
		return event.Type
	}
	if value := strings.TrimSpace(eventError.Type); value != "" {
		return sanitizeClass(value)
	}
	if value := strings.TrimSpace(eventError.Message); value != "" {
		return messageClass(value)
	}
	return sanitizeClass(strings.TrimSpace(event.Type))
}
