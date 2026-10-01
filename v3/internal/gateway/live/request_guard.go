package live

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"golang.org/x/net/websocket"
)

func (h *Handler) requestGuardFailure(ctx context.Context, req *gateway.Request) *gateway.UpstreamError {
	if h.cfg.RequestGuard == nil {
		return nil
	}
	err := h.cfg.RequestGuard(ctx, req)
	if err == nil {
		return nil
	}
	var failure *gateway.UpstreamError
	if errors.As(err, &failure) {
		return failure
	}
	return &gateway.UpstreamError{Status: http.StatusServiceUnavailable, Type: "api_error", Code: "request_guard_unavailable", Message: "request admission is temporarily unavailable"}
}

func requestGuardErrorPayload(failure *gateway.UpstreamError, socket bool) []byte {
	payload := map[string]any{"error": map[string]string{"type": failure.Type, "code": failure.Code, "message": failure.Message}}
	if socket {
		payload["type"], payload["status"] = "error", failure.Status
	}
	body, _ := json.Marshal(payload)
	return body
}

func writeRequestGuardError(w http.ResponseWriter, failure *gateway.UpstreamError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(failure.Status)
	_, _ = w.Write(requestGuardErrorPayload(failure, false))
}

func socketRequestGuardError(conn *websocket.Conn, failure *gateway.UpstreamError) error {
	return frameCodec.Send(conn, wireFrame{data: requestGuardErrorPayload(failure, true), kind: websocket.TextFrame})
}
