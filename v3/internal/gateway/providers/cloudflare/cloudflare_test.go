package cloudflare_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/cloudflare"
	"github.com/tidwall/gjson"
)

func TestNativeEndpointAuthAndPayload(t *testing.T) {
	body := []byte(`{
		"model":"public","stream":true,"max_tokens":20,"max_completion_tokens":30,
		"temperature":0,"top_p":0.8,"top_k":10,"seed":0,"lora":"adapter",
		"frequency_penalty":0.2,"presence_penalty":0.3,"repetition_penalty":1.1,"user":"metadata",
		"messages":[{"role":"developer","content":"Be brief"},{"role":"user","content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]}],
		"stream_options":{"include_usage":true},"n":1,"logprobs":false
	}`)
	for _, base := range []string{"https://api.cloudflare.com", "https://api.cloudflare.com/client/v4/"} {
		request, err := (cloudflare.Provider{}).BuildRequest(context.Background(), &gateway.Request{
			Protocol: gateway.ProtocolOpenAIChat, Model: "public", Stream: true, Body: body,
		}, gateway.Target{BaseURL: base, Secret: "account_id|test_token", UpstreamModel: "@cf/meta/llama-3.1-8b-instruct"})
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if request.URL.String() != "https://api.cloudflare.com/client/v4/accounts/account_id/ai/run/@cf/meta/llama-3.1-8b-instruct" ||
			request.Header.Get("Authorization") != "Bearer test_token" || request.Header.Get("Content-Type") != "application/json" ||
			request.Header.Get("Accept") != "text/event-stream" || request.Method != http.MethodPost {
			t.Fatalf("incorrect native request: %s %v", request.URL, request.Header)
		}
		for path, want := range map[string]string{
			"messages.0.role": "system", "messages.1.content": "hello world", "max_tokens": "30",
			"temperature": "0", "top_p": "0.8", "top_k": "10", "seed": "0", "lora": "adapter",
			"frequency_penalty": "0.2", "presence_penalty": "0.3", "repetition_penalty": "1.1", "stream": "true",
		} {
			if value := gjson.GetBytes(got, path).String(); value != want {
				t.Fatalf("%s = %q, want %q; body = %s", path, value, want, got)
			}
		}
		for _, field := range []string{"model", "stream_options", "max_completion_tokens", "user", "n", "logprobs"} {
			if gjson.GetBytes(got, field).Exists() {
				t.Fatalf("client-only field %s sent upstream: %s", field, got)
			}
		}
	}
	if gjson.GetBytes(body, "model").String() != "public" {
		t.Fatal("client request was mutated")
	}
	request, err := (cloudflare.Provider{}).BuildRequest(context.Background(), &gateway.Request{
		Protocol: gateway.ProtocolOpenAIChat, Model: "@cf/meta/model", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}, gateway.Target{Secret: "account|token"})
	if err != nil || request.URL.Host != "api.cloudflare.com" {
		t.Fatalf("default endpoint = %v %v", request, err)
	}
}

func TestUnsupportedArgumentsAreRejectedRatherThanDropped(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `not JSON`, `{"messages":[]}`,
		`{"messages":[{"role":"user","content":"hi"}],"tools":[]}`,
		`{"messages":[{"role":"user","content":"hi"}],"stop":"END"}`,
		`{"messages":[{"role":"user","content":"hi"}],"tool_choice":"auto"}`,
		`{"messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_object"}}`,
		`{"messages":[{"role":"user","content":"hi"}],"n":2}`,
		`{"messages":[{"role":"user","content":"hi"}],"logprobs":true}`,
		`{"messages":[{"role":"user","content":"hi"}],"stream_options":{"include_usage":1}}`,
		`{"messages":[{"role":"user","content":"hi"}],"stream_options":{"include_usage":null}}`,
		`{"messages":[{"role":"user","content":"hi"}],"stream_options":{"unknown":true}}`,
		`{"messages":[{"role":"user","content":"hi"}],"stream_options":[]}`,
		`{"messages":[{"role":"user","content":"hi"}],"temperature":true}`,
		`{"messages":[{"role":"user","content":"hi"}],"model":1}`,
		`{"messages":[{"role":"user","content":"hi"}],"stream":"true"}`,
		`{"messages":[{"role":"user","content":"hi"}],"user":{}}`,
		`{"messages":[{"role":"user","content":"hi"}],"temperature":-1}`,
		`{"messages":[{"role":"user","content":"hi"}],"top_p":1.1}`,
		`{"messages":[{"role":"user","content":"hi"}],"max_tokens":0}`,
		`{"messages":[{"role":"user","content":"hi"}],"max_completion_tokens":1.5}`,
		`{"messages":[{"role":"user","content":"hi"}],"repetition_penalty":0}`,
		`{"messages":[{"role":"user","content":"hi"}],"seed":-1}`,
		`{"messages":[{"role":"user","content":"hi","name":"speaker"}]}`,
		`{"messages":[{"role":"tool","content":"hi"}]}`,
		`{"messages":[{"role":"assistant","content":null,"tool_calls":[]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/i.png"}}]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}]}`,
		`{"messages":[{"role":"user","content":{}}]}`,
		`{"messages":[{"content":"missing role"}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			_, err := (cloudflare.Provider{}).BuildRequest(context.Background(), &gateway.Request{
				Protocol: gateway.ProtocolOpenAIChat, Model: "@cf/meta/model", Body: []byte(body),
			}, gateway.Target{BaseURL: "https://example.com", Secret: "account|token"})
			var upstream *gateway.UpstreamError
			if !errors.As(err, &upstream) || upstream.Status != http.StatusBadRequest || upstream.Code != "unsupported_request" {
				t.Fatalf("request error = %v", err)
			}
		})
	}
}

func TestInvalidCredentialsProtocolAndRouting(t *testing.T) {
	request := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "@cf/meta/model", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}
	for _, secret := range []string{"", "token", "|token", "account|", "../account|token", "account|token|extra", "account|token\r\ninjected: yes"} {
		_, err := (cloudflare.Provider{}).BuildRequest(context.Background(), request, gateway.Target{Secret: secret})
		var upstream *gateway.UpstreamError
		if !errors.As(err, &upstream) || upstream.Status != http.StatusBadGateway || upstream.Code != "invalid_credentials" || strings.Contains(upstream.Message, "injected: yes") {
			t.Fatalf("credential validation/leak = %v", err)
		}
	}
	for _, model := range []string{"", "../other", "@cf/model?token=oops", "/empty", "@cf//model", "@cf/meta/../model", "@cf/model%2Fother"} {
		_, err := (cloudflare.Provider{}).BuildRequest(context.Background(), request, gateway.Target{Secret: "account|token", UpstreamModel: model})
		if model == "" { // No mapping intentionally uses the request model.
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("unsafe model path %q accepted", model)
		}
	}
	for _, base := range []string{"ftp://example.com", "https://user:pass@example.com", "https://example.com?secret=query", "https://example.com#fragment", "example.com"} {
		_, err := (cloudflare.Provider{}).BuildRequest(context.Background(), request, gateway.Target{Secret: "account|token", BaseURL: base})
		var upstream *gateway.UpstreamError
		if !errors.As(err, &upstream) || upstream.Status != http.StatusBadGateway || upstream.Code != "invalid_base_url" {
			t.Fatalf("invalid base URL %q not classified as channel error: %v", base, err)
		}
	}
	_, err := (cloudflare.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolGemini}, gateway.Target{})
	var upstream *gateway.UpstreamError
	if !errors.As(err, &upstream) || upstream.Code != "unsupported_protocol" {
		t.Fatalf("protocol validation = %v", err)
	}
}
