package gateway

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
)

func TestFastServiceTierScope(t *testing.T) {
	for _, tc := range []struct {
		protocol         Protocol
		path, body, want string
	}{
		{ProtocolResponses, "/v1/responses", `{"service_tier":"fast"}`, "fast"},
		{ProtocolResponses, "/v1/responses/compact", `{"service_tier":"priority"}`, "priority"},
		{ProtocolOpenAIChat, "/v1/chat/completions", `{"service_tier":"priority"}`, "priority"},
		{ProtocolOpenAIChat, "/v1/embeddings", `{"service_tier":"fast"}`, ""},
		{ProtocolOpenAIChat, "/v1/audio/speech", `{"service_tier":"fast"}`, ""},
		{ProtocolAnthropic, "/v1/messages", `{"service_tier":"priority"}`, ""},
		{ProtocolResponses, "/v1/responses", `{"speed":"fast","reasoning":{"effort":"high"}}`, ""},
		{ProtocolResponses, "/v1/responses", `{"service_tier":"auto"}`, ""},
	} {
		req := &Request{Protocol: tc.protocol, Path: tc.path, Body: []byte(tc.body)}
		if got := FastServiceTier(req); got != tc.want {
			t.Errorf("%s %s: got %q, want %q", tc.path, tc.body, got, tc.want)
		}
	}
}

func TestFastChannelPolicyKeepsBillingAndUpstreamConsistent(t *testing.T) {
	for _, tc := range []struct {
		name, tier, path string
		override         map[string]any
		wantError        bool
	}{
		{"preserve fast", "fast", "/v1/responses", nil, false},
		{"override cannot silently downgrade", "priority", "/v1/responses", map[string]any{"service_tier": "default"}, false},
		{"anthropic unsupported", "fast", "/v1/messages", nil, true},
		{"override cannot silently upgrade", "default", "/v1/responses", map[string]any{"service_tier": "fast"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"model","input":"hello","service_tier":"` + tc.tier + `"}`)
			req := &Request{Protocol: ProtocolResponses, Path: "/v1/responses", Body: bytes.Clone(body)}
			out, err := http.NewRequest(http.MethodPost, "https://upstream.example"+tc.path, bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = out.Body.Close() }()
			out.Header.Set("Content-Type", "application/json")
			err = ApplyUpstreamRequest(out, req, Target{Settings: map[string]any{"allow_service_tier": false}, ParamOverride: tc.override})
			if tc.wantError {
				if err == nil {
					t.Fatal("accepted a tier inconsistent with the reservation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			forwarded, err := io.ReadAll(out.Body)
			if err != nil {
				t.Fatal(err)
			}
			if got := gjson.GetBytes(forwarded, "service_tier").Str; got != tc.tier {
				t.Fatalf("tier=%q, want %q", got, tc.tier)
			}
			if !bytes.Equal(req.Body, body) {
				t.Fatal("mutated client pricing input")
			}
		})
	}
}

func TestLatestUpstreamTierWinsEvenWithoutNewUsage(t *testing.T) {
	out := Decide(Observation{Delivered: true, Usage: &Usage{PromptTokens: 10, ServiceTier: "fast"}, ServiceTier: "default"})
	if !out.Charge || out.Usage.ServiceTier != "default" || out.Usage.PromptTokens != 10 {
		t.Fatalf("outcome=%+v", out)
	}
	out = Decide(Observation{Delivered: true, Estimate: Usage{PromptTokens: 10, Estimated: true}, ServiceTier: "default"})
	if !out.Charge || out.Usage.ServiceTier != "default" || !out.Usage.Estimated {
		t.Fatalf("estimated outcome=%+v", out)
	}
}
