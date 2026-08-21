package capability

import (
	"context"
	"net/http"
	"strings"
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
)

const remoteCompactionProbeTimeout = 30 * time.Second

func probeRemoteCompactionV1(ctx context.Context, client *http.Client, endpoint string, input ProbeInput, base gatewayschema.CapabilityProbeState) gatewayschema.CapabilityProbeState {
	probeCtx, cancel := context.WithTimeout(ctx, remoteCompactionProbeTimeout)
	defer cancel()
	body := map[string]any{
		"model":        input.Model,
		"input":        "Reply with OK.",
		"instructions": "",
	}
	status, raw, err := requestJSON(probeCtx, client, http.MethodPost, endpoint, input.APIKey, body)
	return compactionProbeState(base, status, raw, err)
}

func probeRemoteCompactionV2(ctx context.Context, client *http.Client, endpoint string, input ProbeInput, base gatewayschema.CapabilityProbeState) gatewayschema.CapabilityProbeState {
	probeCtx, cancel := context.WithTimeout(ctx, remoteCompactionProbeTimeout)
	defer cancel()
	body := map[string]any{
		"model": input.Model,
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "Reply with OK."},
			map[string]any{"type": "compaction_trigger"},
		},
		"stream": true,
		"store":  true,
	}
	response, err := requestWithHeaders(probeCtx, client, http.MethodPost, endpoint, input.APIKey, body, "application/json, text/event-stream", http.Header{
		"X-Codex-Beta-Features": []string{"remote_compaction_v2"},
	})
	if err != nil {
		return compactionProbeState(base, 0, nil, err)
	}
	defer response.Body.Close()
	raw, readErr := readProbeBody(response)
	if readErr != nil {
		return compactionProbeState(base, response.StatusCode, raw, readErr)
	}
	state := compactionProbeState(base, response.StatusCode, raw, nil)
	if state.Status == gatewayschema.CapabilityStatusSupported && !strings.Contains(strings.ToLower(string(raw)), "compaction") {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		state.ErrorClass = "missing_compaction_signal"
	}
	return state
}

func compactionProbeState(base gatewayschema.CapabilityProbeState, status int, raw []byte, err error) gatewayschema.CapabilityProbeState {
	state := base
	state.HTTPStatus = status
	if err != nil {
		state.Status = gatewayschema.CapabilityStatusError
		state.ErrorClass = classifyTransportError(err)
		return state
	}
	if status >= 200 && status < 300 {
		state.Status = gatewayschema.CapabilityStatusSupported
		state.ErrorClass = ""
		return state
	}
	state.ErrorClass = responseErrorClass(raw, status)
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusGone || status == http.StatusNotImplemented || strings.Contains(strings.ToLower(state.ErrorClass), "unsupported") {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		return state
	}
	state.Status = gatewayschema.CapabilityStatusError
	return state
}
