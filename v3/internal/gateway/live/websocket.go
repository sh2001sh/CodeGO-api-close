package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

type wireFrame struct {
	data []byte
	kind byte
}

var frameCodec = websocket.Codec{
	Marshal: func(v any) ([]byte, byte, error) {
		f, ok := v.(wireFrame)
		if !ok {
			return nil, 0, errors.New("live: invalid frame")
		}
		return f.data, f.kind, nil
	},
	Unmarshal: func(data []byte, kind byte, v any) error {
		f, ok := v.(*wireFrame)
		if !ok {
			return errors.New("live: invalid frame receiver")
		}
		f.data = bytes.Clone(data)
		f.kind = kind
		return nil
	},
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func acceptWebSocket(w http.ResponseWriter, r *http.Request, fn func(*websocket.Conn)) {
	if !isWebSocket(r) {
		w.Header().Set("Upgrade", "websocket")
		writeError(w, 426, "websocket_required", "WebSocket upgrade is required")
		return
	}
	s := websocket.Server{Handshake: func(cfg *websocket.Config, r *http.Request) error {
		cfg.Protocol = nil
		for _, p := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
			if strings.TrimSpace(p) == "realtime" {
				cfg.Protocol = []string{"realtime"}
			}
		}
		return nil
	}, Handler: websocket.Handler(fn)}
	s.ServeHTTP(w, r)
}

func socketError(conn *websocket.Conn, status int, code, message string) error {
	data, _ := json.Marshal(map[string]any{"type": "error", "status": status, "error": map[string]string{"type": "invalid_request_error", "code": code, "message": message}})
	return frameCodec.Send(conn, wireFrame{data: data, kind: websocket.TextFrame})
}

func (h *Handler) serveResponsesWebSocket(w http.ResponseWriter, r *http.Request) {
	_, ok := h.authorize(w, r)
	if !ok {
		return
	}
	acceptWebSocket(w, r, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(r.Context(), h.cfg.SessionTimeout)
		defer cancel()
		defer func() { _ = conn.Close() }()
		deadline, _ := ctx.Deadline()
		_ = conn.SetDeadline(deadline)
		conn.MaxPayloadBytes = int(h.cfg.MaxBodyBytes)
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		state := &responseState{}
		var pinned *gateway.Target
		for ctx.Err() == nil {
			if h.handleResponsesWebSocketTurn(ctx, conn, r, state, &pinned) {
				return
			}
		}
	})
}

// handleResponsesWebSocketTurn receives and processes a single client frame on an open Responses
// WebSocket session: invalid or policy-rejected frames are reported over the socket and the
// caller should keep looping; a receive failure, prewarm send failure, or a terminal/timeout
// outcome from executing the turn means the connection is done and the caller must return.
func (h *Handler) handleResponsesWebSocketTurn(ctx context.Context, conn *websocket.Conn, r *http.Request, state *responseState, pinned **gateway.Target) (done bool) {
	var frame wireFrame
	if err := frameCodec.Receive(conn, &frame); err != nil {
		return true
	}
	turn, err := state.normalize(frame.data)
	if err != nil {
		status, code := 400, "invalid_request"
		var protocolErr *responseProtocolError
		if errors.As(err, &protocolErr) {
			status, code = protocolErr.status, protocolErr.code
		}
		return socketError(conn, status, code, err.Error()) != nil
	}
	principal, ok := h.authorizeSocket(ctx, conn, r, gjson.GetBytes(turn.body, "model").Str)
	if !ok {
		return true
	}
	if handled, done := h.sendWebSocketPrewarm(conn, state, turn); handled {
		return done
	}
	req, ok := h.buildResponsesWebSocketRequest(ctx, conn, r, principal, turn)
	if !ok {
		return false
	}
	if !h.resolveWebSocketTurnTargets(ctx, conn, turn, req, pinned) {
		return false
	}
	if err := h.cfg.Settler.Reserve(ctx, req); err != nil {
		status := 503
		if errors.Is(err, gateway.ErrInsufficientCredits) {
			status = 402
		}
		_ = socketError(conn, status, "billing_unavailable", "unable to reserve credits")
		return false
	}
	id, output, complete, out := h.responseTurn(ctx, conn, req)
	h.finalize(req, out)
	if out.Target != nil && out.Delivered {
		target := *out.Target
		*pinned = &target
	}
	if complete {
		state.complete(turn, id, output)
	} else {
		state.fail(turn)
	}
	return out.Terminal == gateway.TerminalClientCanceled || out.Terminal == gateway.TerminalTimeout
}

// sendWebSocketPrewarm sends a prewarm turn's queued payloads to the client and records it as
// complete, without ever reaching upstream. handled is true when turn was a prewarm turn, in
// which case done tells the caller whether the connection must now be closed (a send failure).
func (h *Handler) sendWebSocketPrewarm(conn *websocket.Conn, state *responseState, turn *responseTurn) (handled, done bool) {
	if len(turn.prewarmPayloads) == 0 {
		return false, false
	}
	for _, data := range turn.prewarmPayloads {
		if frameCodec.Send(conn, wireFrame{data: data, kind: websocket.TextFrame}) != nil {
			return true, true
		}
	}
	id := gjson.GetBytes(turn.prewarmPayloads[len(turn.prewarmPayloads)-1], "response.id").Str
	state.complete(turn, id, []byte("[]"))
	return true, false
}

// buildResponsesWebSocketRequest builds the gateway.Request for a non-prewarm turn and runs it
// through the request guard, forwarding the small set of client headers the upstream protocol
// needs. ok is false if the guard rejects the request (already reported over the socket).
func (h *Handler) buildResponsesWebSocketRequest(ctx context.Context, conn *websocket.Conn, r *http.Request, principal gateway.Principal, turn *responseTurn) (req *gateway.Request, ok bool) {
	req = &gateway.Request{ID: requestID(), Received: time.Now(), Protocol: gateway.ProtocolResponses, Body: turn.body, Model: gjson.GetBytes(turn.body, "model").Str, Path: r.URL.Path, Stream: true, Principal: principal, PricingHeaders: backgroundPricingHeaders(r.Header)}
	if failure := h.requestGuardFailure(ctx, req); failure != nil {
		_ = socketRequestGuardError(conn, failure)
		return req, false
	}
	req.ClientHeaders = make(map[string]string)
	for _, name := range []string{"OpenAI-Beta", "X-Codex-Turn-State"} {
		if value := r.Header.Get(name); value != "" {
			req.ClientHeaders[name] = value
		}
	}
	return req, true
}

// resolveWebSocketTurnTargets pins the turn to its previous response's route if continuing a
// prior generation, plans eligible targets (filtering to the pin when present), and checks
// target policy for the first candidate. Any failure is reported over the socket and ok is
// false, telling the caller to keep looping without executing the turn.
func (h *Handler) resolveWebSocketTurnTargets(ctx context.Context, conn *websocket.Conn, turn *responseTurn, req *gateway.Request, pinned **gateway.Target) (ok bool) {
	if previous := gjson.GetBytes(turn.body, "previous_response_id").Str; previous != "" {
		_, channel, credential, e := h.previousResponse(ctx, req, previous)
		if e != nil {
			_ = socketError(conn, 404, "previous_response_not_found", "previous response was not found")
			return false
		}
		target, e := h.cfg.Resolve(ctx, channel, credential)
		if e != nil {
			_ = socketError(conn, 503, "route_unavailable", "response route is unavailable")
			return false
		}
		*pinned = &target
	}
	targets, err := h.Plan(ctx, req)
	if err != nil {
		_ = socketError(conn, 503, "no_available_channel", "no channel available")
		return false
	}
	if *pinned != nil {
		filtered := targets[:0]
		for _, t := range targets {
			if t.ChannelID == (*pinned).ChannelID && t.CredentialID == (*pinned).CredentialID {
				filtered = append(filtered, t)
			}
		}
		targets = filtered
	}
	if len(targets) == 0 {
		_ = socketError(conn, 503, "no_available_channel", "no channel available for this session and model")
		return false
	}
	req.Targets = targets
	if failure := h.targetPolicyFailure(req, targets[0]); failure != nil {
		_ = socketError(conn, failure.Status, failure.Code, failure.Message)
		return false
	}
	return true
}
