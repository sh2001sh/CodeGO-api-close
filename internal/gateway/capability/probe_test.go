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
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp_test","status":"completed"}`))
	}))
	defer server.Close()

	result := ProbeResponsesTransportsForCandidates(context.Background(), []ProbeInput{
		{BaseURL: server.URL + "/v1", APIKey: "key", Model: "bad-model", KeyIndex: 0},
		{BaseURL: server.URL + "/v1", APIKey: "key", Model: "good-model", KeyIndex: 0},
	})

	require.Equal(t, []string{"bad-model", "good-model", "good-model", "good-model"}, plainModels)
	require.Equal(t, "good-model", result.WebSocket.Model)
	require.Equal(t, gatewayschema.CapabilityStatusUnsupported, result.WebSocket.Status)
	require.Equal(t, gatewayschema.CapabilityStatusUnsupported, result.NativeBackground.Status)
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
