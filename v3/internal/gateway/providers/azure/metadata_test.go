package azure_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/azure"
)

func TestChannelAPIVersionPreservesV2Semantics(t *testing.T) {
	for _, tc := range []struct {
		name, base, want string
		protocol         gateway.Protocol
		settings         map[string]any
	}{
		{"chat configured", "https://azure.example", "2025-03-01-preview", gateway.ProtocolOpenAIChat, map[string]any{"api_version": "2025-03-01-preview"}},
		{"chat query wins", "https://azure.example?api-version=query-version", "query-version", gateway.ProtocolOpenAIChat, map[string]any{"api_version": "channel-version"}},
		{"chat empty defaults", "https://azure.example", "2024-10-21", gateway.ProtocolOpenAIChat, map[string]any{"api_version": ""}},
		{"chat null defaults", "https://azure.example", "2024-10-21", gateway.ProtocolOpenAIChat, map[string]any{"api_version": nil}},
		{"ordinary responses keep preview", "https://azure.example", "preview", gateway.ProtocolResponses, map[string]any{"api_version": "chat-only-version"}},
		{"cognitive responses configured", "https://test.cognitiveservices.azure.com", "2025-03-01-preview", gateway.ProtocolResponses, map[string]any{"api_version": "2025-03-01-preview"}},
		{"ordinary responses override", "https://azure.example", "responses-version", gateway.ProtocolResponses, map[string]any{"api_version": "chat-only-version", "azure_responses_version": "responses-version"}},
		{"cognitive responses override", "https://test.cognitiveservices.azure.com", "responses-version", gateway.ProtocolResponses, map[string]any{"api_version": "channel-version", "azure_responses_version": "responses-version"}},
		{"responses query wins", "https://azure.example?api-version=query-version", "query-version", gateway.ProtocolResponses, map[string]any{"azure_responses_version": "responses-version"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := (azure.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: tc.protocol, Model: "deploy", Body: []byte(`{"model":"deploy","messages":[{"role":"user","content":"hi"}],"input":"hi"}`)}, gateway.Target{BaseURL: tc.base, Secret: "test", Settings: tc.settings})
			if err != nil {
				t.Fatal(err)
			}
			if got := out.URL.Query().Get("api-version"); got != tc.want {
				t.Fatalf("api-version=%q, want %q; url=%s", got, tc.want, out.URL)
			}
		})
	}
}

func TestInvalidChannelVersionFailsWithoutSilentDefault(t *testing.T) {
	for _, field := range []string{"api_version", "azure_responses_version"} {
		for _, value := range []any{true, 42, []any{"v1"}, map[string]any{"default": "v1"}, "version\r\ninvalid"} {
			_, err := (azure.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolResponses, Model: "deploy", Body: []byte(`{}`)}, gateway.Target{BaseURL: "https://azure.example", Secret: "credential-not-in-error", Settings: map[string]any{field: value}})
			var upstream *gateway.UpstreamError
			if !errors.As(err, &upstream) || upstream.Status != http.StatusBadGateway || upstream.Code != "invalid_channel_api_version" {
				t.Fatalf("field %s value %T error=%v", field, value, err)
			}
		}
	}
}
