package capability

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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

func probeNativeBackground(ctx context.Context, client *http.Client, endpoint string, input ProbeInput, base gatewayschema.CapabilityProbeState) gatewayschema.CapabilityProbeState {
	state := base
	create := map[string]any{
		"model": input.Model, "input": "Reply with OK.",
		"max_output_tokens": 16, "background": true, "store": true,
	}
	status, raw, err := requestJSON(ctx, client, http.MethodPost, endpoint, input.APIKey, create)
	state.HTTPStatus = status
	if err != nil || status < 200 || status >= 300 {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		if err != nil {
			state.ErrorClass = classifyTransportError(err)
		} else {
			state.ErrorClass = responseErrorClass(raw, status)
		}
		return state
	}
	var created responseEnvelope
	if platformencoding.Unmarshal(raw, &created) != nil || created.ID == "" {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		state.ErrorClass = "background_create_invalid"
		return state
	}
	status, raw, err = requestJSON(ctx, client, http.MethodGet, endpoint+"/"+url.PathEscape(created.ID), input.APIKey, nil)
	if err != nil || status < 200 || status >= 300 {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		state.HTTPStatus = status
		state.ErrorClass = "background_retrieve_failed"
		return state
	}
	var retrieved responseEnvelope
	if platformencoding.Unmarshal(raw, &retrieved) != nil || retrieved.ID != created.ID {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		state.ErrorClass = "background_retrieve_invalid"
		return state
	}
	if !probeBackgroundResume(ctx, client, endpoint, input) {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		state.ErrorClass = "background_resume_failed"
		return state
	}
	if !probeBackgroundCancel(ctx, client, endpoint, input) {
		state.Status = gatewayschema.CapabilityStatusUnsupported
		state.ErrorClass = "background_cancel_failed"
		return state
	}
	state.Status = gatewayschema.CapabilityStatusSupported
	state.ErrorClass = ""
	return state
}

func probeBackgroundResume(ctx context.Context, client *http.Client, endpoint string, input ProbeInput) bool {
	body := map[string]any{
		"model": input.Model, "input": "Write three short numbered lines.",
		"max_output_tokens": 64, "background": true, "stream": true, "store": true,
	}
	response, err := request(ctx, client, http.MethodPost, endpoint, input.APIKey, body, "text/event-stream")
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
	resumed, err := request(ctx, client, http.MethodGet, resumeURL, input.APIKey, nil, "text/event-stream")
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
	status, raw, err := requestJSON(ctx, client, http.MethodPost, endpoint, input.APIKey, body)
	if err != nil || status < 200 || status >= 300 {
		return false
	}
	var created responseEnvelope
	if platformencoding.Unmarshal(raw, &created) != nil || created.ID == "" {
		return false
	}
	status, raw, err = requestJSON(ctx, client, http.MethodPost, endpoint+"/"+url.PathEscape(created.ID)+"/cancel", input.APIKey, map[string]any{})
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
