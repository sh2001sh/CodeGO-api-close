package live

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/net/websocket"
)

func (h *Handler) forwardRealtime(ctx context.Context, client, upstream *websocket.Conn, r *http.Request, req *gateway.Request, target gateway.Target, accounting *realtimeAccounting) relayEnd {
	for {
		var frame wireFrame
		if err := frameCodec.Receive(client, &frame); err != nil {
			return relayEnd{client: true, err: err}
		}
		root := gjson.ParseBytes(frame.data)
		// Recheck even audio input: VAD can start a response without an
		// explicit response.create frame from the client.
		principal, ok := h.authorizeSocket(ctx, client, r, req.Model)
		if !ok {
			return relayEnd{client: true}
		}
		policyRequest := &gateway.Request{ID: req.ID, Received: req.Received, Protocol: req.Protocol,
			Path: req.Path, Body: req.Body, Model: req.Model, Stream: true,
			Principal: principal, PricingHeaders: req.PricingHeaders}
		if failure := h.targetPolicyFailure(policyRequest, target); failure != nil {
			_ = socketRequestGuardError(client, failure)
			return relayEnd{client: true}
		}
		path := ""
		switch root.Get("type").Str {
		case "session.update":
			path = "session.model"
		case "response.create":
			path = "response.model"
		}
		if path != "" {
			model := root.Get(path)
			if model.Exists() && model.Str != req.Model && model.Str != target.UpstreamModel {
				if err := socketError(client, 400, "model_change_forbidden", "a realtime connection cannot change its routed model"); err != nil {
					return relayEnd{client: true, err: err}
				}
				continue
			}
			if model.Exists() && target.UpstreamModel != "" {
				frame.data, _ = sjson.SetBytes(frame.data, path, target.UpstreamModel)
			}
			prepared, err := realtimeFrameRequest(ctx, req, target, frame.data)
			if err != nil {
				if err := socketError(client, 400, "realtime_override_invalid", "channel realtime request override could not be applied"); err != nil {
					return relayEnd{client: true, err: err}
				}
				continue
			}
			frame.data = prepared
		}
		accounting.mu.Lock()
		if accounting.req == nil {
			accounting.mu.Unlock()
			return relayEnd{client: true}
		}
		if root.Get("type").Str == "response.create" && accounting.pending {
			accounting.mu.Unlock()
			if err := socketError(client, 409, "response_in_progress", "wait for the current realtime response before creating another"); err != nil {
				return relayEnd{client: true, err: err}
			}
			continue
		}
		if root.Get("type").Str == "response.create" {
			accounting.pending = true
		}
		err := frameCodec.Send(upstream, frame)
		accounting.mu.Unlock()
		if err != nil {
			return relayEnd{err: err}
		}
	}
}

func realtimeFrameRequest(ctx context.Context, req *gateway.Request, target gateway.Target, data []byte) ([]byte, error) {
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://realtime.invalid/v1/realtime", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", "application/json")
	target.HeaderOverride = nil // handshake headers already use channel policy
	if err := gateway.ApplyUpstreamRequest(out, req, target); err != nil {
		_ = out.Body.Close()
		return nil, err
	}
	defer func() { _ = out.Body.Close() }()
	return io.ReadAll(out.Body)
}
