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

const responsesCapabilityProbeTimeout = 4 * time.Minute

type ProbeProtocol string

const (
	ProbeProtocolOpenAIResponses ProbeProtocol = "openai_responses"
	ProbeProtocolCodexResponses  ProbeProtocol = "codex_responses"
	ProbeProtocolNotApplicable   ProbeProtocol = "not_applicable"
)

var probeSlots = make(chan struct{}, 6)

type ProbeInput struct {
	BaseURL       string
	APIKey        string
	Model         string
	KeyIndex      int
	ResponsesPath string
	CompactPath   string
	Protocol      ProbeProtocol
	Headers       http.Header
	SkipReason    string
}

type ProbeResult struct {
	WebSocket          gatewayschema.CapabilityProbeState
	NativeBackground   gatewayschema.CapabilityProbeState
	BackgroundCreate   gatewayschema.CapabilityProbeState
	BackgroundResume   gatewayschema.CapabilityProbeState
	BackgroundCancel   gatewayschema.CapabilityProbeState
	RemoteCompactionV1 gatewayschema.CapabilityProbeState
	RemoteCompactionV2 gatewayschema.CapabilityProbeState
}

func ProbeResponsesTransports(ctx context.Context, input ProbeInput) ProbeResult {
	return ProbeResponsesTransportsForCandidates(ctx, []ProbeInput{input})
}

// ProbeResponsesTransportsForCandidates probes every capability independently.
// A broken ordinary Responses request, model, or key therefore cannot suppress
// transport and compaction evidence from the remaining candidates.
func ProbeResponsesTransportsForCandidates(ctx context.Context, candidates []ProbeInput) ProbeResult {
	select {
	case probeSlots <- struct{}{}:
		defer func() { <-probeSlots }()
	case <-ctx.Done():
		return probeInputError(firstProbeInput(candidates), "probe_canceled")
	}
	probeCtx, cancel := context.WithTimeout(ctx, responsesCapabilityProbeTimeout)
	defer cancel()

	if len(candidates) == 0 {
		return probeInputError(ProbeInput{}, "no_probe_candidates")
	}
	if candidates[0].Protocol == ProbeProtocolNotApplicable {
		reason := strings.TrimSpace(candidates[0].SkipReason)
		if reason == "" {
			reason = "protocol_not_applicable"
		}
		return unsupportedProbeResult(candidates[0], reason)
	}

	client := &http.Client{Timeout: 25 * time.Second}
	stateResults := make(chan struct {
		name  string
		state gatewayschema.CapabilityProbeState
	}, 3)
	go func() {
		stateResults <- struct {
			name  string
			state gatewayschema.CapabilityProbeState
		}{"v1", probeStateCandidates(probeCtx, client, candidates, true, probeRemoteCompactionV1)}
	}()
	go func() {
		stateResults <- struct {
			name  string
			state gatewayschema.CapabilityProbeState
		}{"v2", probeStateCandidates(probeCtx, client, candidates, false, probeRemoteCompactionV2)}
	}()
	go func() {
		stateResults <- struct {
			name  string
			state gatewayschema.CapabilityProbeState
		}{"websocket", probeStateCandidates(probeCtx, client, candidates, false, probeWebSocketCandidate)}
	}()
	backgroundResults := make(chan backgroundProbeResult, 1)
	go func() { backgroundResults <- probeBackgroundCandidates(probeCtx, client, candidates) }()

	result := ProbeResult{}
	for range 3 {
		probeResult := <-stateResults
		switch probeResult.name {
		case "v1":
			result.RemoteCompactionV1 = probeResult.state
		case "v2":
			result.RemoteCompactionV2 = probeResult.state
		case "websocket":
			result.WebSocket = probeResult.state
		}
	}
	background := <-backgroundResults
	result.NativeBackground = background.Aggregate
	result.BackgroundCreate = background.Create
	result.BackgroundResume = background.Resume
	result.BackgroundCancel = background.Cancel
	return result
}

func PendingResponsesCapabilities(model string) gatewayschema.ResponsesCapabilities {
	now := time.Now().Unix()
	state := gatewayschema.CapabilityProbeState{
		Status:    gatewayschema.CapabilityStatusPending,
		CheckedAt: now,
		Model:     strings.TrimSpace(model),
	}
	return gatewayschema.ResponsesCapabilities{WebSocket: state, NativeBackground: state, BackgroundCreate: state, BackgroundResume: state, BackgroundCancel: state, RemoteCompactionV1: state, RemoteCompactionV2: state}
}

func probeEndpoints(input ProbeInput) (string, string, error) {
	responsesPath := strings.TrimSpace(input.ResponsesPath)
	if responsesPath == "" {
		responsesPath = "/v1/responses"
	}
	compactPath := strings.TrimSpace(input.CompactPath)
	if compactPath == "" {
		compactPath = strings.TrimSuffix(responsesPath, "/responses") + "/responses/compact"
	}
	responses, err := endpointWithPath(input.BaseURL, responsesPath)
	if err != nil {
		return "", "", err
	}
	compact, err := endpointWithPath(input.BaseURL, compactPath)
	if err != nil {
		return "", "", err
	}
	return responses, compact, nil
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

func endpointWithPath(baseURL, requestPath string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errInvalidBaseURL
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	basePath := strings.TrimRight(parsed.Path, "/")
	relative := "/" + strings.TrimLeft(strings.TrimSpace(requestPath), "/")
	if strings.HasSuffix(basePath, "/v1") && strings.HasPrefix(relative, "/v1/") {
		relative = strings.TrimPrefix(relative, "/v1")
	}
	parsed.Path = strings.TrimRight(basePath, "/") + relative
	return parsed.String(), nil
}

func probeInputError(input ProbeInput, class string) ProbeResult {
	state := gatewayschema.CapabilityProbeState{
		Status:      gatewayschema.CapabilityStatusError,
		CheckedAt:   time.Now().Unix(),
		Model:       strings.TrimSpace(input.Model),
		ErrorClass:  class,
		ProbeKeyIdx: input.KeyIndex,
	}
	return ProbeResult{WebSocket: state, NativeBackground: state, BackgroundCreate: state, BackgroundResume: state, BackgroundCancel: state, RemoteCompactionV1: state, RemoteCompactionV2: state}
}

func unsupportedProbeResult(input ProbeInput, class string) ProbeResult {
	state := gatewayschema.CapabilityProbeState{
		Status: gatewayschema.CapabilityStatusUnsupported, CheckedAt: time.Now().Unix(),
		Model: strings.TrimSpace(input.Model), ErrorClass: class, ProbeKeyIdx: input.KeyIndex,
	}
	return ProbeResult{WebSocket: state, NativeBackground: state, BackgroundCreate: state, BackgroundResume: state, BackgroundCancel: state, RemoteCompactionV1: state, RemoteCompactionV2: state}
}

func firstProbeInput(candidates []ProbeInput) ProbeInput {
	if len(candidates) == 0 {
		return ProbeInput{}
	}
	return candidates[0]
}
