package gateway_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestAuxiliaryPromptPolicyUsesActualPathText(t *testing.T) {
	policy := gateway.NewSensitiveWordPolicy(func() map[string]json.RawMessage {
		return map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`"boundary-marker"`)}
	}, nil)
	tests := []struct {
		path string
		body string
	}{
		{"/v1/embeddings", `{"input":["allowed","boundary-marker"]}`},
		{"/v1/engines/vector/embeddings", `{"input":"boundary-marker"}`},
		{"/v1/audio/speech", `{"input":"boundary-marker"}`},
		{"/v1/images/generations", `{"prompt":"boundary-marker"}`},
		{"/v1/images/edits", `{"prompt":"boundary-marker"}`},
		{"/v1/completions", `{"prompt":["boundary-marker"]}`},
		{"/v1/edits", `{"input":"allowed","instruction":"boundary-marker"}`},
		{"/v1/moderations", `{"input":"boundary-marker"}`},
		{"/v1/rerank", `{"query":"allowed","documents":[{"text":"boundary-marker"}]}`},
		{"/v1beta/models/vector:embedContent", `{"content":{"parts":[{"text":"boundary-marker"}]}}`},
		{"/v1/models/vector:batchEmbedContents", `{"requests":[{"content":{"parts":[{"text":"boundary-marker"}]}}]}`},
		{"/v1beta/models/image:predict", `{"instances":[{"prompt":"boundary-marker"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			err := policy(&gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Path: tt.path, Body: []byte(tt.body)}, gateway.Target{})
			var refusal *gateway.UpstreamError
			if !errors.As(err, &refusal) || refusal.Status != 403 {
				t.Fatalf("prompt was not refused: %v", err)
			}
		})
	}
}

func TestAuxiliaryPromptPolicyDoesNotScanMetadataOrTrustOperationHeaders(t *testing.T) {
	policy := gateway.NewSensitiveWordPolicy(func() map[string]json.RawMessage {
		return map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`"boundary-marker"`)}
	}, nil)
	for _, path := range []string{"/v1/embeddings", "/v1/audio/speech", "/v1/images/edits", "/v1/rerank", "/v1beta/models/vector:embedContent"} {
		request := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Path: path,
			Body: []byte(`{"input":[23,45],"prompt":"allowed","documents":[{"schema":"boundary-marker"}],"content":{"parts":[{"fileData":{"fileUri":"https://example.org/boundary-marker"}}]},"metadata":{"note":"boundary-marker"}}`)}
		if err := policy(request, gateway.Target{}); err != nil {
			t.Fatalf("metadata triggered policy on %s: %v", path, err)
		}
	}
	request := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Path: "/v1/chat/completions",
		PricingHeaders: map[string]string{"X-Codego-Operation": "embeddings"},
		Body:           []byte(`{"messages":[{"role":"user","content":"boundary-marker"}],"input":"allowed"}`)}
	if err := policy(request, gateway.Target{}); err == nil {
		t.Fatal("client operation header bypassed actual Chat prompt")
	}
}
