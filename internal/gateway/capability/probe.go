package capability

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
)

const responsesWebSocketBeta = "responses_websockets=2026-02-06"

var probeSlots = make(chan struct{}, 2)

type ProbeInput struct {
	BaseURL  string
	APIKey   string
	Model    string
	KeyIndex int
}

type ProbeResult struct {
	WebSocket        gatewayschema.CapabilityProbeState
	NativeBackground gatewayschema.CapabilityProbeState
	BackgroundCreate gatewayschema.CapabilityProbeState
	BackgroundResume gatewayschema.CapabilityProbeState
	BackgroundCancel gatewayschema.CapabilityProbeState
}

func ProbeResponsesTransports(ctx context.Context, input ProbeInput) ProbeResult {
	select {
	case probeSlots <- struct{}{}:
		defer func() { <-probeSlots }()
	case <-ctx.Done():
		return probeInputError(input, "probe_canceled")
	}

	checkedAt := time.Now().Unix()
	base := gatewayschema.CapabilityProbeState{
		CheckedAt:   checkedAt,
		Model:       strings.TrimSpace(input.Model),
		ProbeKeyIdx: input.KeyIndex,
	}
	result := ProbeResult{WebSocket: base, NativeBackground: base, BackgroundCreate: base, BackgroundResume: base, BackgroundCancel: base}
	endpoint, err := responsesEndpoint(input.BaseURL)
	if err != nil || strings.TrimSpace(input.APIKey) == "" || base.Model == "" {
		result.WebSocket.Status = gatewayschema.CapabilityStatusError
		result.WebSocket.ErrorClass = "invalid_probe_input"
		result.NativeBackground.Status = gatewayschema.CapabilityStatusError
		result.NativeBackground.ErrorClass = "invalid_probe_input"
		result.BackgroundCreate = result.NativeBackground
		result.BackgroundResume = result.NativeBackground
		result.BackgroundCancel = result.NativeBackground
		return result
	}

	client := &http.Client{Timeout: 75 * time.Second}
	if status, errorClass := probePlainResponses(ctx, client, endpoint, input.APIKey, base.Model); status < 200 || status >= 300 {
		result.WebSocket.Status = gatewayschema.CapabilityStatusError
		result.WebSocket.HTTPStatus = status
		result.WebSocket.ErrorClass = errorClass
		result.NativeBackground.Status = gatewayschema.CapabilityStatusError
		result.NativeBackground.HTTPStatus = status
		result.NativeBackground.ErrorClass = errorClass
		result.BackgroundCreate = result.NativeBackground
		result.BackgroundResume = result.NativeBackground
		result.BackgroundCancel = result.NativeBackground
		return result
	}

	result.WebSocket = probeWebSocket(ctx, endpoint, input, base)
	background := probeNativeBackground(ctx, client, endpoint, input, base)
	result.NativeBackground = background.Aggregate
	result.BackgroundCreate = background.Create
	result.BackgroundResume = background.Resume
	result.BackgroundCancel = background.Cancel
	return result
}

// ProbeResponsesTransportsForCandidates tries key/model pairings until the
// upstream proves that its ordinary Responses endpoint is callable.
func ProbeResponsesTransportsForCandidates(ctx context.Context, candidates []ProbeInput) ProbeResult {
	var last ProbeResult
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return probeInputError(candidate, "probe_canceled")
		}
		last = ProbeResponsesTransports(ctx, candidate)
		if !plainResponsesProbeFailed(last) {
			return last
		}
	}
	if len(candidates) == 0 {
		return probeInputError(ProbeInput{}, "no_probe_candidates")
	}
	return last
}

func PendingResponsesCapabilities(model string) gatewayschema.ResponsesCapabilities {
	now := time.Now().Unix()
	state := gatewayschema.CapabilityProbeState{
		Status:    gatewayschema.CapabilityStatusPending,
		CheckedAt: now,
		Model:     strings.TrimSpace(model),
	}
	return gatewayschema.ResponsesCapabilities{WebSocket: state, NativeBackground: state, BackgroundCreate: state, BackgroundResume: state, BackgroundCancel: state}
}

func responsesEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errInvalidBaseURL
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/v1/responses"):
	case strings.HasSuffix(path, "/v1"):
		path += "/responses"
	default:
		path += "/v1/responses"
	}
	parsed.Path = path
	return parsed.String(), nil
}

func plainResponsesProbeFailed(result ProbeResult) bool {
	return result.WebSocket.Status == gatewayschema.CapabilityStatusError &&
		result.NativeBackground.Status == gatewayschema.CapabilityStatusError
}

func probeInputError(input ProbeInput, class string) ProbeResult {
	state := gatewayschema.CapabilityProbeState{
		Status:      gatewayschema.CapabilityStatusError,
		CheckedAt:   time.Now().Unix(),
		Model:       strings.TrimSpace(input.Model),
		ErrorClass:  class,
		ProbeKeyIdx: input.KeyIndex,
	}
	return ProbeResult{WebSocket: state, NativeBackground: state, BackgroundCreate: state, BackgroundResume: state, BackgroundCancel: state}
}
