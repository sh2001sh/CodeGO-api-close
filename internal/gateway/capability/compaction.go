package capability

import (
	"bufio"
	"context"
	"encoding/json"
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
		"model": input.Model,
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "Respond with OK."},
		},
		"instructions":        "You are a helpful coding assistant.",
		"parallel_tool_calls": false,
		"text":                map[string]any{"format": map[string]any{"type": "text"}},
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
		"stream":              true,
		"store":               false,
		"instructions":        "You are a helpful coding assistant.",
		"parallel_tool_calls": false,
		"text":                map[string]any{"format": map[string]any{"type": "text"}},
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
	if state.Status == gatewayschema.CapabilityStatusSupported {
		hasCompaction, hasCompleted := parseRemoteCompactionV2Probe(raw)
		if !hasCompaction || !hasCompleted {
			state.Status = gatewayschema.CapabilityStatusUnsupported
			state.ErrorClass = "missing_compaction_terminal"
		}
	}
	return state
}

// parseRemoteCompactionV2Probe validates the protocol-level success signal.
// A 2xx response alone is insufficient: Codex requires a compaction output
// item and a response.completed terminal event in the streamed response.
func parseRemoteCompactionV2Probe(raw []byte) (hasCompaction, hasCompleted bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 1024), 2<<20)
	var data strings.Builder
	flush := func() {
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "" || payload == "[DONE]" {
			return
		}
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
			} `json:"item"`
			Response struct {
				Output []struct {
					Type string `json:"type"`
				} `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(payload), &event) != nil {
			return
		}
		switch event.Type {
		case "response.output_item.done":
			if event.Item.Type == "compaction" || event.Item.Type == "compaction_summary" {
				hasCompaction = true
			}
		case "response.completed", "response.done":
			hasCompleted = true
			for _, item := range event.Response.Output {
				if item.Type == "compaction" || item.Type == "compaction_summary" {
					hasCompaction = true
				}
			}
		}
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	return hasCompaction, hasCompleted
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
