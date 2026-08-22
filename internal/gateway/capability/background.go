package capability

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

type backgroundStreamEvent struct {
	Type           string `json:"type"`
	SequenceNumber int64  `json:"sequence_number"`
	Response       struct {
		ID string `json:"id"`
	} `json:"response"`
}

type backgroundProbeResult struct {
	Aggregate gatewayschema.CapabilityProbeState
	Create    gatewayschema.CapabilityProbeState
	Resume    gatewayschema.CapabilityProbeState
	Cancel    gatewayschema.CapabilityProbeState
}

const backgroundProbeTimeout = 30 * time.Second

func probeNativeBackground(ctx context.Context, client *http.Client, endpoint string, input ProbeInput, base gatewayschema.CapabilityProbeState) backgroundProbeResult {
	probeCtx, cancel := context.WithTimeout(ctx, backgroundProbeTimeout)
	defer cancel()
	result := backgroundProbeResult{Aggregate: base, Create: base, Resume: base, Cancel: base}
	state := base
	create := map[string]any{
		"model": input.Model, "input": "Reply with OK.",
		"max_output_tokens": 16, "background": true, "store": true,
	}
	status, raw, err := requestJSONWithHeaders(probeCtx, client, http.MethodPost, endpoint, input.APIKey, create, probeHeaders(input, nil))
	state.HTTPStatus = status
	if err != nil || status < 200 || status >= 300 {
		state.Status = classifyBackgroundCapabilityFailure(raw, status, err)
		if err != nil {
			state.ErrorClass = classifyTransportError(err)
		} else {
			state.ErrorClass = responseErrorClass(raw, status)
		}
		result.Create = state
		result.Aggregate = state
		result.Resume = state
		result.Cancel = state
		return result
	}
	var created responseEnvelope
	if platformencoding.Unmarshal(raw, &created) != nil || created.ID == "" {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		state.ErrorClass = "background_create_invalid"
		result.Create, result.Aggregate = state, state
		result.Resume, result.Cancel = state, state
		return result
	}
	createState := state
	createState.Status = gatewayschema.CapabilityStatusSupported
	createState.ErrorClass = ""
	result.Create = createState
	status, raw, err = requestJSONWithHeaders(probeCtx, client, http.MethodGet, endpoint+"/"+url.PathEscape(created.ID), input.APIKey, nil, probeHeaders(input, nil))
	if err != nil || status < 200 || status >= 300 {
		state.Status = classifyBackgroundCapabilityFailure(raw, status, err)
		state.HTTPStatus = status
		state.ErrorClass = "background_retrieve_failed"
		result.Aggregate = state
		result.Resume = state
		result.Cancel = state
		return result
	}
	var retrieved responseEnvelope
	if platformencoding.Unmarshal(raw, &retrieved) != nil || retrieved.ID != created.ID {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		state.ErrorClass = "background_retrieve_invalid"
		result.Aggregate = state
		result.Resume, result.Cancel = state, state
		return result
	}
	state.Status = gatewayschema.CapabilityStatusSupported
	state.ErrorClass = ""
	result.Create = createState
	result.Resume = state
	result.Cancel = state
	if !probeBackgroundResume(probeCtx, client, endpoint, input) {
		result.Resume.Status = gatewayschema.CapabilityStatusError
		result.Resume.ErrorClass = "background_resume_failed"
	}
	if !probeBackgroundCancel(probeCtx, client, endpoint, input) {
		result.Cancel.Status = gatewayschema.CapabilityStatusError
		result.Cancel.ErrorClass = "background_cancel_failed"
	}
	result.Aggregate = state
	if result.Resume.Status != gatewayschema.CapabilityStatusSupported || result.Cancel.Status != gatewayschema.CapabilityStatusSupported {
		result.Aggregate.Status = gatewayschema.CapabilityStatusError
		result.Aggregate.ErrorClass = "background_lifecycle_incomplete"
	}
	return result
}

func classifyBackgroundCapabilityFailure(raw []byte, status int, err error) string {
	if err != nil {
		return gatewayschema.CapabilityStatusError
	}
	class := responseErrorClass(raw, status)
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented || strings.Contains(class, "unsupported") {
		return gatewayschema.CapabilityStatusUnsupported
	}
	return gatewayschema.CapabilityStatusError
}

func probeBackgroundResume(ctx context.Context, client *http.Client, endpoint string, input ProbeInput) bool {
	body := map[string]any{
		"model": input.Model, "input": "Write three short numbered lines.",
		"max_output_tokens": 64, "background": true, "stream": true, "store": true,
	}
	response, err := requestWithHeaders(ctx, client, http.MethodPost, endpoint, input.APIKey, body, "text/event-stream", probeHeaders(input, nil))
	if err != nil {
		return false
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return false
	}
	event, ok := firstBackgroundEvent(response.Body)
	response.Body.Close()
	if !ok || event.Response.ID == "" {
		return false
	}
	resumeURL := endpoint + "/" + url.PathEscape(event.Response.ID) + "?stream=true&starting_after=" + strconv.FormatInt(event.SequenceNumber, 10)
	resumed, err := requestWithHeaders(ctx, client, http.MethodGet, resumeURL, input.APIKey, nil, "text/event-stream", probeHeaders(input, nil))
	if err != nil {
		return false
	}
	defer resumed.Body.Close()
	return resumed.StatusCode >= 200 && resumed.StatusCode < 300 && hasLaterBackgroundEvent(resumed.Body, event.SequenceNumber)
}

func probeBackgroundCancel(ctx context.Context, client *http.Client, endpoint string, input ProbeInput) bool {
	body := map[string]any{
		"model":             input.Model,
		"input":             "Write a detailed technical essay about distributed systems.",
		"max_output_tokens": 512, "background": true, "store": true,
	}
	status, raw, err := requestJSONWithHeaders(ctx, client, http.MethodPost, endpoint, input.APIKey, body, probeHeaders(input, nil))
	if err != nil || status < 200 || status >= 300 {
		return false
	}
	var created responseEnvelope
	if platformencoding.Unmarshal(raw, &created) != nil || created.ID == "" {
		return false
	}
	status, raw, err = requestJSONWithHeaders(ctx, client, http.MethodPost, endpoint+"/"+url.PathEscape(created.ID)+"/cancel", input.APIKey, map[string]any{}, probeHeaders(input, nil))
	if err != nil || status < 200 || status >= 300 {
		return false
	}
	var canceled responseEnvelope
	return platformencoding.Unmarshal(raw, &canceled) == nil && canceled.ID == created.ID
}

func firstBackgroundEvent(reader io.Reader) (backgroundStreamEvent, bool) {
	return readBackgroundEvent(reader, func(event backgroundStreamEvent) bool { return event.Response.ID != "" })
}

func hasLaterBackgroundEvent(reader io.Reader, sequence int64) bool {
	_, ok := readBackgroundEvent(reader, func(event backgroundStreamEvent) bool { return event.SequenceNumber > sequence })
	return ok
}

func readBackgroundEvent(reader io.Reader, accept func(backgroundStreamEvent) bool) (backgroundStreamEvent, bool) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event backgroundStreamEvent
		if platformencoding.Unmarshal([]byte(data), &event) == nil && accept(event) {
			return event, true
		}
	}
	return backgroundStreamEvent{}, false
}
