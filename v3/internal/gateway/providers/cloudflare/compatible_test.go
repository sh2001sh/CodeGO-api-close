package cloudflare_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/cloudflare"
	"github.com/tidwall/gjson"
)

func TestCompatibleCredentialsAndAccountEndpoints(t *testing.T) {
	for _, tc := range []struct{ name, base, secret, wantBase string }{
		{"default", "", "account|token", "https://api.cloudflare.com/client/v4/accounts/account/ai"},
		{"root", "https://cf.example", "account|token", "https://cf.example/client/v4/accounts/account/ai"},
		{"client_v4", "https://cf.example/client/v4/", "account|token", "https://cf.example/client/v4/accounts/account/ai"},
		{"json_token", "https://cf.example", `{"account_id":"account","token":"token"}`, "https://cf.example/client/v4/accounts/account/ai"},
		{"json_api_key", "https://cf.example", `{"account_id":"account","api_key":"token"}`, "https://cf.example/client/v4/accounts/account/ai"},
		{"full_base", "https://cf.example/client/v4/accounts/account/ai/", "account|token", "https://cf.example/client/v4/accounts/account/ai"},
		{"full_token", "https://cf.example/client/v4/accounts/account/ai", "token", "https://cf.example/client/v4/accounts/account/ai"},
		{"full_v1", "https://cf.example/client/v4/accounts/account/ai/v1/", "token", "https://cf.example/client/v4/accounts/account/ai"},
		{"proxy_prefix", "https://cf.example/proxy/client/v4", "account|token", "https://cf.example/proxy/client/v4/accounts/account/ai"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, api := range []struct {
				provider gateway.Provider
				protocol gateway.Protocol
				path     string
			}{
				{cloudflare.ChatProvider{}, gateway.ProtocolOpenAIChat, "/v1/chat/completions"},
				{cloudflare.ResponsesProvider{}, gateway.ProtocolResponses, "/v1/responses"},
			} {
				in := &gateway.Request{Protocol: api.protocol, Model: "public", Body: []byte(`{"model":"public","input":"hello","messages":[{"role":"user","content":"hello"}]}`)}
				target := gateway.Target{BaseURL: tc.base, Secret: tc.secret, UpstreamModel: "@cf/meta/llama-3.1-8b-instruct"}
				request, err := api.provider.BuildRequest(context.Background(), in, target)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				if request.URL.String() != tc.wantBase+api.path || request.Header.Get("Authorization") != "Bearer token" || request.Method != http.MethodPost ||
					request.Header.Get("Content-Type") != "application/json" || gjson.GetBytes(body, "model").String() != target.UpstreamModel {
					t.Fatalf("account-scoped request = %s %v body=%s", request.URL, request.Header, body)
				}
				if target.BaseURL != tc.base || target.Secret != tc.secret || gjson.GetBytes(in.Body, "model").String() != "public" {
					t.Fatal("original credential, target or client request mutated")
				}
			}
		})
	}
}

func TestCompatibleAccountConfigurationRejectsInvalidOrConflictingCredentials(t *testing.T) {
	for _, tc := range []struct{ base, secret, code string }{
		{"https://cf.example", "", "invalid_credentials"},
		{"https://cf.example", "token", "invalid_credentials"},
		{"https://cf.example", "account|", "invalid_credentials"},
		{"https://cf.example", "account|token|extra", "invalid_credentials"},
		{"https://cf.example", "account|token\rinjection", "invalid_credentials"},
		{"https://cf.example", "account|token\x00injection", "invalid_credentials"},
		{"https://cf.example", "../account|token", "invalid_credentials"},
		{"https://cf.example", `{"account_id":"account","token":4}`, "invalid_credentials"},
		{"https://cf.example", `{`, "invalid_credentials"},
		{"https://cf.example/client/v4/accounts/account/ai", "different|token", "invalid_credentials"},
		{"https://cf.example/client/v4/accounts/../ai", "token", "invalid_base_url"},
		{"https://cf.example/client/v4/accounts/other%2Faccount/ai", "token", "invalid_base_url"},
		{"https://cf.example/client/v4/accounts/account/ai/unexpected", "token", "invalid_base_url"},
		{"ftp://cf.example", "account|token", "invalid_base_url"},
		{"https://user:pass@cf.example", "account|token", "invalid_base_url"},
		{"https://cf.example?secret=query", "account|token", "invalid_base_url"},
		{"https://cf.example?", "account|token", "invalid_base_url"},
		{"https://cf.example#fragment", "account|token", "invalid_base_url"},
	} {
		for _, api := range []struct {
			provider gateway.Provider
			protocol gateway.Protocol
		}{
			{cloudflare.ChatProvider{}, gateway.ProtocolOpenAIChat},
			{cloudflare.ResponsesProvider{}, gateway.ProtocolResponses},
		} {
			_, err := api.provider.BuildRequest(context.Background(), &gateway.Request{Protocol: api.protocol, Body: []byte(`{"model":"public"}`)}, gateway.Target{BaseURL: tc.base, Secret: tc.secret})
			var upstream *gateway.UpstreamError
			if !errors.As(err, &upstream) || upstream.Status != http.StatusBadGateway || upstream.Code != tc.code || strings.Contains(upstream.Message, "injection") || strings.Contains(upstream.Message, "token|extra") {
				t.Fatalf("invalid account configuration = %v; want %s", err, tc.code)
			}
		}
	}
	for _, api := range []gateway.Provider{cloudflare.ChatProvider{}, cloudflare.ResponsesProvider{}} {
		_, err := api.BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolAnthropic}, gateway.Target{})
		var upstream *gateway.UpstreamError
		if !errors.As(err, &upstream) || upstream.Status != http.StatusBadRequest || upstream.Code != "unsupported_protocol" {
			t.Fatalf("protocol rejection = %v", err)
		}
	}
}

func TestCompatibleChatMockPreservesToolsImagesAndStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if request.URL.Path != "/client/v4/accounts/account/ai/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer token" ||
			request.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Chat account route/auth = %s %v", request.URL, request.Header)
		}
		for path, want := range map[string]string{
			"model": "@cf/meta/model", "messages.0.role": "developer", "messages.1.content.0.text": "look",
			"messages.1.content.1.image_url.url": "https://example.com/image.png", "messages.1.content.1.image_url.detail": "high",
			"messages.2.tool_calls.0.function.arguments": `{"city":"Tokyo"}`, "messages.3.tool_call_id": "call_1",
			"tools.0.function.name": "lookup", "tools.0.function.parameters.type": "object", "tools.0.function.strict": "true",
			"tool_choice.function.name": "lookup", "parallel_tool_calls": "false", "stop.0": "END", "response_format.type": "json_object",
			"stream_options.include_usage": "true", "max_completion_tokens": "30",
		} {
			if got := gjson.GetBytes(body, path).String(); got != want {
				t.Errorf("Chat %s = %q, want %q", path, got, want)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+`{"id":"chatcmpl_cf","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`+"\n\n"+
			"data: [DONE]\n\n")
	}))
	defer server.Close()
	body := []byte(`{"model":"public","stream":true,"max_completion_tokens":30,"messages":[
		{"role":"developer","content":"instructions"},
		{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.com/image.png","detail":"high"}}]},
		{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Tokyo\"}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"sunny"}],
		"tools":[{"type":"function","function":{"name":"lookup","strict":true,"parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"lookup"}},"parallel_tool_calls":false,"stop":["END"],"response_format":{"type":"json_object"}}`)
	in := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "public", Stream: true, Body: body}
	provider := cloudflare.ChatProvider{}
	request, err := provider.BuildRequest(context.Background(), in, gateway.Target{BaseURL: server.URL, Secret: "account|token", UpstreamModel: "@cf/meta/model"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	s := provider.Decode(in, response)
	defer func() { _ = s.Close() }()
	first, err := s.Next()
	if err != nil || first.Kind != gateway.EventData || first.TextBytes != 5 || gjson.GetBytes(first.Payload, "choices.0.delta.content").String() != "hello" {
		t.Fatalf("Chat semantic output = %+v %v", first, err)
	}
	last, err := s.Next()
	if err != nil || last.Kind != gateway.EventData || last.Usage == nil || last.Usage.PromptTokens != 4 || last.Usage.CompletionTokens != 2 || last.Usage.Estimated {
		t.Fatalf("Chat real usage = %+v %v", last, err)
	}
	if done, err := s.Next(); err != nil || done.Kind != gateway.EventDone {
		t.Fatalf("Chat done = %+v %v", done, err)
	}
	if gjson.GetBytes(body, "model").String() != "public" || gjson.GetBytes(body, "stream_options").Exists() {
		t.Fatal("original Chat body mutated")
	}
}

func responsesEvent(name, body string) string {
	return "event: " + name + "\ndata: " + body + "\n\n"
}

func TestCompatibleResponsesMockPreservesNativeLifecycleUsageErrorAndCut(t *testing.T) {
	created := responsesEvent("response.created", `{"type":"response.created","response":{"id":"resp_cf"}}`)
	delta := responsesEvent("response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`)
	for _, tc := range []struct{ name, wire, terminal string }{
		{"completed", created + delta + responsesEvent("response.completed", `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":2,"input_tokens_details":{"cached_tokens":4}}}}`), "response.completed"},
		{"failed", delta + responsesEvent("response.failed", `{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"failed"},"usage":{"input_tokens":10,"output_tokens":2}}}`), "response.failed"},
		{"cut", delta, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if request.URL.Path != "/client/v4/accounts/account/ai/v1/responses" || request.Header.Get("Authorization") != "Bearer token" ||
					request.Header.Get("Accept") != "text/event-stream" {
					t.Errorf("Responses account route/auth = %s %v", request.URL, request.Header)
				}
				for path, want := range map[string]string{
					"model": "@cf/meta/model", "previous_response_id": "resp_previous", "instructions": "Be brief", "tools.0.type": "web_search",
					"input.0.content.0.text": "hello", "input.0.content.1.image_url": "https://example.com/image.png", "include.0": "reasoning.encrypted_content",
				} {
					if got := gjson.GetBytes(body, path).String(); got != want {
						t.Errorf("Responses %s = %q, want %q", path, got, want)
					}
				}
				if gjson.GetBytes(body, "stream_options").Exists() {
					t.Error("Chat stream_options leaked into Responses payload")
				}
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				_, _ = io.WriteString(w, tc.wire)
			}))
			defer server.Close()
			in := &gateway.Request{Protocol: gateway.ProtocolResponses, Model: "public", Stream: true, Body: []byte(`{"model":"public","stream":true,"previous_response_id":"resp_previous","instructions":"Be brief","tools":[{"type":"web_search"}],"include":["reasoning.encrypted_content"],"input":[{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"https://example.com/image.png"}]}]}`)}
			provider := cloudflare.ResponsesProvider{}
			request, err := provider.BuildRequest(context.Background(), in, gateway.Target{BaseURL: server.URL + "/client/v4/accounts/account/ai", Secret: "token", UpstreamModel: "@cf/meta/model"})
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			s := provider.Decode(in, response)
			defer func() { _ = s.Close() }()
			if tc.name == "completed" {
				event, err := s.Next()
				if err != nil || event.Kind != gateway.EventData || event.Name != "response.created" {
					t.Fatalf("created lifecycle = %+v %v", event, err)
				}
			}
			event, err := s.Next()
			if err != nil || event.Kind != gateway.EventData || event.Name != "response.output_text.delta" || event.TextBytes != 5 || gjson.GetBytes(event.Payload, "delta").String() != "hello" {
				t.Fatalf("semantic Responses event = %+v %v", event, err)
			}
			event, err = s.Next()
			if tc.name == "cut" {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("Responses truncation = %+v %v", event, err)
				}
				return
			}
			if err != nil || event.Name != tc.terminal || event.Usage == nil || event.Usage.PromptTokens != 10 || event.Usage.CompletionTokens != 2 || event.Usage.Estimated {
				t.Fatalf("Responses terminal usage = %+v %v", event, err)
			}
			if tc.name == "failed" {
				if event.Kind != gateway.EventError || event.Err == nil || event.Err.Code != "server_error" || event.Err.Message != "failed" {
					t.Fatalf("Responses native failure = %+v", event)
				}
				return
			}
			if event.Kind != gateway.EventData || event.Usage.CachedTokens != 4 {
				t.Fatalf("completed Responses = %+v", event)
			}
			if done, err := s.Next(); err != nil || done.Kind != gateway.EventDone {
				t.Fatalf("Responses done = %+v %v", done, err)
			}
			if _, err := s.Next(); !errors.Is(err, io.EOF) {
				t.Fatalf("Responses EOF = %v", err)
			}
		})
	}
}

func TestCompatibleResponsesNonStreamingKeepsResponseShape(t *testing.T) {
	s := (cloudflare.ResponsesProvider{}).Decode(&gateway.Request{Protocol: gateway.ProtocolResponses}, &http.Response{
		Header: http.Header{"Content-Type": {"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"id":"resp_cf","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":1}}`)),
	})
	defer func() { _ = s.Close() }()
	event, err := s.Next()
	if err != nil || event.Kind != gateway.EventData || event.Usage == nil || event.Usage.PromptTokens != 3 || event.Usage.CompletionTokens != 1 ||
		gjson.GetBytes(event.Payload, "id").String() != "resp_cf" || gjson.GetBytes(event.Payload, "output.0.content.0.text").String() != "hello" || gjson.GetBytes(event.Payload, "choices").Exists() {
		t.Fatalf("non-stream Responses = %+v %v", event, err)
	}
}
