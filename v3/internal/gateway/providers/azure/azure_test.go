package azure_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/azure"
)

func TestAzureRequestsAndNoDuplicateBase(t *testing.T) {
	for _, tc := range []struct {
		name, base, path, version string
		protocol                  gateway.Protocol
	}{
		{"chat", "https://azure.example", "/openai/deployments/deploy-2/chat/completions", "2024-10-21", gateway.ProtocolOpenAIChat},
		{"chat openai", "https://azure.example/openai/", "/openai/deployments/deploy-2/chat/completions", "2024-10-21", gateway.ProtocolOpenAIChat},
		{"chat full", "https://azure.example/openai/deployments/old/chat/completions?api-version=custom", "/openai/deployments/deploy-2/chat/completions", "custom", gateway.ProtocolOpenAIChat},
		{"responses", "https://azure.example/openai/v1", "/openai/v1/responses", "preview", gateway.ProtocolResponses},
		{"responses full", "https://azure.example/openai/v1/responses?api-version=v1", "/openai/v1/responses", "v1", gateway.ProtocolResponses},
		{"cognitive", "https://example.cognitiveservices.azure.com", "/openai/responses", "2024-10-21", gateway.ProtocolResponses},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := []byte(`{"model":"public","stream":true,"input":"hello","messages":[{"role":"user","content":"hi"}]}`)
			r, err := (azure.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: tc.protocol, Model: "public", Body: original, Stream: true}, gateway.Target{BaseURL: tc.base, Secret: "test-key", UpstreamModel: "deploy-2"})
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if r.URL.Path != tc.path || r.URL.Query().Get("api-version") != tc.version || r.Header.Get("Api-Key") != "test-key" || r.Header.Get("Authorization") != "" {
				t.Fatalf("request = %s %v", r.URL, r.Header)
			}
			if !strings.Contains(string(body), `"model":"deploy-2"`) || !strings.Contains(string(original), `"model":"public"`) {
				t.Fatalf("model/original = %s / %s", body, original)
			}
			if tc.protocol == gateway.ProtocolOpenAIChat && !strings.Contains(string(body), `"include_usage":true`) {
				t.Fatalf("usage not requested: %s", body)
			}
		})
	}
}

func TestAzureRejectsUnsupportedProtocolAndBadURL(t *testing.T) {
	for _, tc := range []struct {
		protocol gateway.Protocol
		base     string
	}{
		{gateway.ProtocolGemini, "https://azure.example"}, {gateway.ProtocolOpenAIChat, "azure.example"},
	} {
		_, err := (azure.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: tc.protocol, Model: "test", Body: []byte(`{}`)}, gateway.Target{BaseURL: tc.base})
		if err == nil {
			t.Fatal("invalid request accepted")
		}
	}
}

func TestAzureDecodePreservesBillingUsage(t *testing.T) {
	for _, tc := range []struct {
		protocol gateway.Protocol
		body     string
	}{
		{gateway.ProtocolOpenAIChat, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":7,"completion_tokens":2}}`},
		{gateway.ProtocolResponses, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":7,"output_tokens":2}}`},
	} {
		s := (azure.Provider{}).Decode(&gateway.Request{Protocol: tc.protocol}, &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))})
		ev, err := s.Next()
		_ = s.Close()
		if err != nil || ev.Usage == nil || ev.Usage.PromptTokens != 7 || ev.Usage.CompletionTokens != 2 {
			t.Fatalf("event = %+v %v", ev, err)
		}
	}
}

func TestAzureStreamUsageAndInBandError(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":2,\"prompt_tokens_details\":{\"cached_tokens\":3}}}\n\n" +
		"data: {\"error\":{\"code\":\"content_filter\",\"message\":\"blocked\"}}\n\n"
	s := (azure.Provider{}).Decode(&gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Body: []byte(`{}`)}, &http.Response{
		Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body)),
	})
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.TextBytes != 5 || ev.Kind != gateway.EventData {
		t.Fatalf("text = %+v %v", ev, err)
	}
	ev, err = s.Next()
	if err != nil || ev.Kind != gateway.EventUsage || ev.Usage == nil || ev.Usage.PromptTokens != 8 || ev.Usage.CachedTokens != 3 {
		t.Fatalf("usage = %+v %v", ev, err)
	}
	ev, err = s.Next()
	if err != nil || ev.Kind != gateway.EventError || ev.Err == nil || ev.Err.Code != "content_filter" {
		t.Fatalf("error = %+v %v", ev, err)
	}
}
