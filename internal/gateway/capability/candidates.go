package capability

import (
	"context"
	"net/http"
	"strings"
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
)

type candidateStateProbe func(context.Context, *http.Client, string, ProbeInput, gatewayschema.CapabilityProbeState) gatewayschema.CapabilityProbeState

// IsResponsesProbeModel excludes modalities that cannot exercise the text
// Responses transports or remote compaction protocol.
func IsResponsesProbeModel(model string) bool {
	value := strings.ToLower(strings.TrimSpace(model))
	for _, marker := range []string{"image", "embedding", "rerank", "whisper", "tts", "suno", "video"} {
		if strings.Contains(value, marker) {
			return false
		}
	}
	return value != ""
}

func probeStateCandidates(ctx context.Context, client *http.Client, candidates []ProbeInput, useCompactEndpoint bool, probe candidateStateProbe) gatewayschema.CapabilityProbeState {
	last := invalidCandidateState(firstProbeInput(candidates), "no_probe_candidates")
	lastError := last
	var unsupported *gatewayschema.CapabilityProbeState
	hadError := false
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return invalidCandidateState(candidate, "probe_canceled")
		}
		endpoint, compactEndpoint, base, ok := prepareProbeCandidate(candidate)
		if !ok {
			last = base
			continue
		}
		target := endpoint
		if useCompactEndpoint {
			target = compactEndpoint
		}
		last = probe(ctx, client, target, candidate, base)
		if last.Status == gatewayschema.CapabilityStatusSupported {
			return last
		}
		if last.Status == gatewayschema.CapabilityStatusUnsupported {
			copy := last
			unsupported = &copy
			continue
		}
		hadError = true
		if preferCapabilityError(last, lastError) {
			lastError = last
		}
	}
	if unsupported != nil && !hadError {
		return *unsupported
	}
	if hadError {
		return lastError
	}
	return last
}

func preferCapabilityError(candidate, current gatewayschema.CapabilityProbeState) bool {
	if current.ErrorClass == "no_probe_candidates" {
		return true
	}
	if candidate.HandshakeStatus == gatewayschema.CapabilityStatusSupported && current.HandshakeStatus != gatewayschema.CapabilityStatusSupported {
		return true
	}
	if candidate.HandshakeStatus != gatewayschema.CapabilityStatusSupported && current.HandshakeStatus == gatewayschema.CapabilityStatusSupported {
		return false
	}
	return candidate.CheckedAt >= current.CheckedAt
}

func probeBackgroundCandidates(ctx context.Context, client *http.Client, candidates []ProbeInput) backgroundProbeResult {
	state := invalidCandidateState(firstProbeInput(candidates), "no_probe_candidates")
	last := backgroundProbeResult{Aggregate: state, Create: state, Resume: state, Cancel: state}
	lastError := last
	var unsupported *backgroundProbeResult
	hadError := false
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			state = invalidCandidateState(candidate, "probe_canceled")
			return backgroundProbeResult{Aggregate: state, Create: state, Resume: state, Cancel: state}
		}
		endpoint, _, base, ok := prepareProbeCandidate(candidate)
		if !ok {
			last = backgroundProbeResult{Aggregate: base, Create: base, Resume: base, Cancel: base}
			continue
		}
		last = probeNativeBackground(ctx, client, endpoint, candidate, base)
		if last.Aggregate.Status == gatewayschema.CapabilityStatusSupported {
			return last
		}
		if last.Aggregate.Status == gatewayschema.CapabilityStatusUnsupported {
			copy := last
			unsupported = &copy
			continue
		}
		hadError = true
		lastError = last
	}
	if unsupported != nil && !hadError {
		return *unsupported
	}
	if hadError {
		return lastError
	}
	return last
}

func prepareProbeCandidate(input ProbeInput) (string, string, gatewayschema.CapabilityProbeState, bool) {
	base := gatewayschema.CapabilityProbeState{CheckedAt: time.Now().Unix(), Model: strings.TrimSpace(input.Model), ProbeKeyIdx: input.KeyIndex}
	endpoint, compactEndpoint, err := probeEndpoints(input)
	if err != nil || strings.TrimSpace(input.APIKey) == "" || base.Model == "" {
		base.Status = gatewayschema.CapabilityStatusError
		base.ErrorClass = "invalid_probe_input"
		return "", "", base, false
	}
	return endpoint, compactEndpoint, base, true
}

func invalidCandidateState(input ProbeInput, class string) gatewayschema.CapabilityProbeState {
	return gatewayschema.CapabilityProbeState{
		Status: gatewayschema.CapabilityStatusError, CheckedAt: time.Now().Unix(),
		Model: strings.TrimSpace(input.Model), ErrorClass: class, ProbeKeyIdx: input.KeyIndex,
	}
}
