package capability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"github.com/stretchr/testify/require"
)

func TestResponsesEndpointAcceptsRootAndVersionedBaseURLs(t *testing.T) {
	for _, testCase := range []struct {
		base string
		want string
	}{
		{base: "https://api.example.com", want: "https://api.example.com/v1/responses"},
		{base: "https://api.example.com/v1", want: "https://api.example.com/v1/responses"},
		{base: "https://api.example.com/v1/", want: "https://api.example.com/v1/responses"},
		{base: "https://api.example.com/v1/responses", want: "https://api.example.com/v1/responses"},
	} {
		endpoint, err := responsesEndpoint(testCase.base)
		require.NoError(t, err)
		require.Equal(t, testCase.want, endpoint)
	}
}

func TestProbeResponsesTransportsForCandidatesTriesNextModel(t *testing.T) {
	var mutex sync.Mutex
	plainModels := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
			http.Error(writer, "websocket unsupported", http.StatusNotImplemented)
			return
		}
		var body map[string]any
		if request.Body != nil {
			_ = platformencoding.DecodeJSON(request.Body, &body)
		}
		if background, _ := body["background"].(bool); background {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"type":"unsupported"}}`))
			return
		}
		model, _ := body["model"].(string)
		mutex.Lock()
		plainModels = append(plainModels, model)
		mutex.Unlock()
		if model == "bad-model" {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"type":"invalid_model"}}`))
			return
		}
		if strings.HasSuffix(request.URL.Path, "/responses/compact") {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id":"cmp_test","object":"response.compaction","output":[{"type":"compaction"}]}`))
			return
		}
		if input, ok := body["input"].([]any); ok {
			for _, item := range input {
				object, _ := item.(map[string]any)
				if object["type"] == "compaction_trigger" {
					writer.Header().Set("Content-Type", "text/event-stream")
					_, _ = writer.Write([]byte("data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\"}}\n\n" +
						"data: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n"))
					return
				}
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp_test","status":"completed"}`))
	}))
	defer server.Close()

	result := ProbeResponsesTransportsForCandidates(context.Background(), []ProbeInput{
		{BaseURL: server.URL + "/v1", APIKey: "key", Model: "bad-model", KeyIndex: 0},
		{BaseURL: server.URL + "/v1", APIKey: "key", Model: "good-model", KeyIndex: 0},
	})

	require.Contains(t, plainModels, "bad-model")
	require.Contains(t, plainModels, "good-model")
	require.Equal(t, "good-model", result.WebSocket.Model)
	require.Equal(t, gatewayschema.CapabilityStatusUnsupported, result.WebSocket.Status)
	require.Equal(t, gatewayschema.CapabilityStatusUnsupported, result.NativeBackground.Status)
	require.Equal(t, gatewayschema.CapabilityStatusSupported, result.RemoteCompactionV1.Status)
	require.Equal(t, gatewayschema.CapabilityStatusSupported, result.RemoteCompactionV2.Status)
}

func TestNotApplicableProtocolDoesNotSendRequests(t *testing.T) {
	result := ProbeResponsesTransportsForCandidates(context.Background(), []ProbeInput{{
		Protocol: ProbeProtocolNotApplicable, Model: "claude-opus-4-6", KeyIndex: -1,
		SkipReason: "protocol_not_applicable",
	}})
	require.Equal(t, gatewayschema.CapabilityStatusUnsupported, result.RemoteCompactionV1.Status)
	require.Equal(t, "protocol_not_applicable", result.RemoteCompactionV1.ErrorClass)
}

func TestParseRemoteCompactionV1ProbeRequiresCompactionEnvelope(t *testing.T) {
	require.True(t, parseRemoteCompactionV1Probe([]byte(`{"id":"cmp","object":"response.compaction"}`)))
	require.True(t, parseRemoteCompactionV1Probe([]byte(`{"output":[{"type":"compaction_summary"}]}`)))
	require.False(t, parseRemoteCompactionV1Probe([]byte(`{"id":"resp","object":"response"}`)))
}

func TestRemoteCompactionV1RejectsPlainSuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp","object":"response","status":"completed"}`))
	}))
	defer server.Close()
	result := ProbeResponsesTransportsForCandidates(context.Background(), []ProbeInput{{
		BaseURL: server.URL, APIKey: "key", Model: "gpt-5", Protocol: ProbeProtocolOpenAIResponses,
	}})
	require.Equal(t, gatewayschema.CapabilityStatusUnsupported, result.RemoteCompactionV1.Status)
	require.Equal(t, "missing_compaction_output", result.RemoteCompactionV1.ErrorClass)
}

func TestParseRemoteCompactionV2ProbeAcceptsJSONCompactionEnvelope(t *testing.T) {
	hasCompaction, hasCompleted := parseRemoteCompactionV2Probe([]byte(`{"id":"cmp","object":"response.compaction"}`))
	require.True(t, hasCompaction)
	require.True(t, hasCompleted)
}

func TestBackgroundTransientFailureIsNotMarkedUnsupported(t *testing.T) {
	state := gatewayschema.CapabilityProbeState{
		Status: gatewayschema.CapabilityStatusError, HTTPStatus: http.StatusGatewayTimeout,
		ErrorClass: "background_resume_failed",
	}
	require.True(t, IsTransientProbeFailure(state))
}

func TestRemoteCompactionCapabilityKeepsAuthenticationAndTransientFailuresRetryable(t *testing.T) {
	capabilities := gatewayschema.ResponsesCapabilities{
		RemoteCompactionV1: gatewayschema.CapabilityProbeState{
			Status: gatewayschema.CapabilityStatusError, HTTPStatus: http.StatusUnauthorized, ErrorClass: "http_401",
		},
		RemoteCompactionV2: gatewayschema.CapabilityProbeState{
			Status: gatewayschema.CapabilityStatusError, HTTPStatus: http.StatusServiceUnavailable, ErrorClass: "http_503",
		},
	}
	require.True(t, capabilities.AllowsRemoteCompactionV1For("", 0))
	require.True(t, capabilities.AllowsRemoteCompactionV2For("", 0))
	require.True(t, IsTransientProbeFailure(capabilities.RemoteCompactionV2))

	capabilities.RemoteCompactionV1.Status = gatewayschema.CapabilityStatusUnsupported
	require.False(t, capabilities.AllowsRemoteCompactionV1For("", 0))
}

func TestParseRemoteCompactionV2ProbeRequiresCompactionAndCompleted(t *testing.T) {
	valid := []byte("data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n")
	hasCompaction, hasCompleted := parseRemoteCompactionV2Probe(valid)
	require.True(t, hasCompaction)
	require.True(t, hasCompleted)

	missingItem := []byte("data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n")
	hasCompaction, hasCompleted = parseRemoteCompactionV2Probe(missingItem)
	require.False(t, hasCompaction)
	require.True(t, hasCompleted)

	missingCompleted := []byte("data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\"}}\n\n")
	hasCompaction, hasCompleted = parseRemoteCompactionV2Probe(missingCompleted)
	require.True(t, hasCompaction)
	require.False(t, hasCompleted)
}

func TestParseRemoteCompactionV2ProbeAcceptsCompactionInCompletedOutput(t *testing.T) {
	raw := []byte("data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"compaction\"}]}}\n\n")
	hasCompaction, hasCompleted := parseRemoteCompactionV2Probe(raw)
	require.True(t, hasCompaction)
	require.True(t, hasCompleted)
}

func TestParseRemoteCompactionV2ProbeDoesNotTreatAddedOnlyAsComplete(t *testing.T) {
	raw := []byte("data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"compaction\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n")
	hasCompaction, hasCompleted := parseRemoteCompactionV2Probe(raw)
	require.False(t, hasCompaction)
	require.True(t, hasCompleted)
}
