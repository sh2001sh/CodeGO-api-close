package coze_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/coze"
	"github.com/tidwall/gjson"
)

func chatRequest(stream bool) *gateway.Request {
	return &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, ID: "test", Model: "client", Stream: stream,
		Received: time.Unix(100, 0), Body: []byte(`{"model":"client","messages":[{"role":"user","content":"hi"}],"stream_options":{"include_usage":true}}`)}
}

func event(name, data string) string { return "event: " + name + "\ndata: " + data + "\n\n" }

func answer(text string) string {
	return event("conversation.message.delta", `{"id":"m1","role":"assistant","type":"answer","content_type":"text","content":"`+text+`"}`)
}

func completed(usage string) string {
	return event("conversation.chat.completed", `{"status":"completed"`+usage+`}`)
}

func decode(t *testing.T, body string, stream bool) gateway.EventStream {
	t.Helper()
	s := (coze.Provider{}).Decode(chatRequest(stream), &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))})
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func readAll(t *testing.T, s gateway.EventStream) []gateway.Event {
	t.Helper()
	var events []gateway.Event
	for {
		ev, err := s.Next()
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, ev)
	}
}

func TestBuildNativeBotChatRequest(t *testing.T) {
	body := `{"model":"client","messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"text","text":"!"}]},{"role":"assistant","content":"hello"},{"role":"user","content":"next"}],"user":"person","conversation_id":"space &?","custom_variables":{"key":"value"},"parameters":{"x":1},"meta_data":{"source":"test"},"extra_params":{"k":"v"},"shortcut_command":{"name":"hello"},"auto_save_history":false,"stream_options":{"include_usage":true},"n":1}`
	for _, secret := range []string{"bot|token", `{"bot_id":"bot","token":"token"}`, `{"bot_id":"bot","access_token":"token"}`, `{"bot_id":"bot","api_key":"token"}`} {
		for _, base := range []string{"https://coze.test", "https://coze.test/v3/", "https://coze.test/v3/chat?foo=bar"} {
			r := chatRequest(false)
			r.Body = []byte(body)
			out, err := (coze.Provider{}).BuildRequest(context.Background(), r, gateway.Target{BaseURL: base, Secret: secret, UpstreamModel: "ignored-bot-model"})
			if err != nil {
				t.Fatal(err)
			}
			if out.URL.Path != "/v3/chat" || out.URL.Query().Get("conversation_id") != "space &?" || out.Method != "POST" || out.Header.Get("Authorization") != "Bearer token" || out.Header.Get("Accept") != "text/event-stream" {
				t.Fatalf("request = %s %v", out.URL, out.Header)
			}
			if strings.Contains(base, "foo=bar") && out.URL.Query().Get("foo") != "bar" {
				t.Fatal("existing query was discarded")
			}
			converted, err := io.ReadAll(out.Body)
			if err != nil {
				t.Fatal(err)
			}
			for path, want := range map[string]string{"bot_id": "bot", "user_id": "person", "additional_messages.0.content": "hi!", "additional_messages.1.role": "assistant", "additional_messages.1.type": "answer", "additional_messages.2.content_type": "text", "custom_variables.key": "value", "parameters.x": "1", "auto_save_history": "false", "stream": "true"} {
				if got := gjson.GetBytes(converted, path).String(); got != want {
					t.Fatalf("%s = %q, want %q; %s", path, got, want, converted)
				}
			}
			if gjson.GetBytes(converted, "model").Exists() || gjson.GetBytes(converted, "stream_options").Exists() || gjson.GetBytes(converted, "conversation_id").Exists() {
				t.Fatalf("native body = %s", converted)
			}
			if string(r.Body) != body {
				t.Fatal("client body mutated")
			}
		}
	}
	r, err := (coze.Provider{}).BuildRequest(context.Background(), chatRequest(true), gateway.Target{Secret: "bot|token"})
	if err != nil {
		t.Fatal(err)
	}
	converted, _ := io.ReadAll(r.Body)
	if r.URL.String() != "https://api.coze.com/v3/chat" || gjson.GetBytes(converted, "user_id").Str != "test" {
		t.Fatalf("default request = %s %s", r.URL, converted)
	}
}

func TestRejectsUnsupportedLossyArguments(t *testing.T) {
	for _, body := range []string{
		`not JSON`, `null`, `[]`, `{"messages":[]}`,
		`{"messages":[{"role":"system","content":"instructions"}]}`,
		`{"messages":[{"role":"tool","content":"result"}]}`,
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://image.test"}}]}]}`,
		`{"messages":[{"role":"assistant","content":null,"tool_calls":[]}]}`,
		`{"messages":[{"role":"user","content":"hi","name":"named"}]}`,
		`{"messages":[{"role":"user","content":"hi"}],"temperature":0}`,
		`{"messages":[{"role":"user","content":"hi"}],"max_tokens":1}`,
		`{"messages":[{"role":"user","content":"hi"}],"tools":[]}`,
		`{"messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_object"}}`,
		`{"messages":[{"role":"user","content":"hi"}],"n":2}`,
		`{"messages":[{"role":"user","content":"hi"}],"user":123}`,
		`{"messages":[{"role":"user","content":"hi"}],"conversation_id":[]}`,
		`{"messages":[{"role":"user","content":"hi"}],"auto_save_history":1}`,
		`{"messages":[{"role":"user","content":"hi"}],"custom_variables":[]}`,
		`{"messages":[{"role":"user","content":"hi"}],"stream_options":{"include_usage":"true"}}`,
		`{"messages":[{"role":"user","content":"hi"}],"stream_options":{"other":true}}`,
	} {
		t.Run(body, func(t *testing.T) {
			r := chatRequest(true)
			r.Body = []byte(body)
			_, err := (coze.Provider{}).BuildRequest(context.Background(), r, gateway.Target{BaseURL: "https://coze.test", Secret: "bot|token"})
			var typed *gateway.UpstreamError
			if !errors.As(err, &typed) || typed.Status != 400 || typed.Code != "unsupported_request" {
				t.Fatalf("error = %v", err)
			}
		})
	}
	_, err := (coze.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolResponses}, gateway.Target{})
	var typed *gateway.UpstreamError
	if !errors.As(err, &typed) || typed.Status != http.StatusBadRequest || typed.Type != "invalid_request_error" || typed.Code != "unsupported_protocol" {
		t.Fatalf("protocol error = %v", err)
	}
}

func TestChannelConfigurationErrorsPermitFailoverAndHideCredentials(t *testing.T) {
	const bot = "sensitive-bot-id"
	const token = "sensitive-config-token"
	for _, tc := range []struct {
		name   string
		target gateway.Target
		code   string
	}{
		{"missing credential", gateway.Target{}, "invalid_credentials"},
		{"token without bot", gateway.Target{Secret: token}, "invalid_credentials"},
		{"missing token", gateway.Target{Secret: bot + "|"}, "invalid_credentials"},
		{"missing bot", gateway.Target{Secret: "|" + token}, "invalid_credentials"},
		{"malformed delimiter", gateway.Target{Secret: bot + "|" + token + "|extra"}, "invalid_credentials"},
		{"header injection", gateway.Target{Secret: bot + "|" + token + "\n"}, "invalid_credentials"},
		{"JSON without token", gateway.Target{Secret: `{"bot_id":"` + bot + `"}`}, "invalid_credentials"},
		{"malformed JSON", gateway.Target{Secret: `{"bot_id":"` + bot + `","token":"` + token}, "invalid_credentials"},
		{"unsupported scheme", gateway.Target{Secret: bot + "|" + token, BaseURL: "ftp://coze.test"}, "invalid_base_url"},
		{"relative URL", gateway.Target{Secret: bot + "|" + token, BaseURL: "/v3/chat"}, "invalid_base_url"},
		{"endpoint credentials", gateway.Target{Secret: bot + "|" + token, BaseURL: "https://" + token + "@coze.test"}, "invalid_base_url"},
		{"endpoint fragment", gateway.Target{Secret: bot + "|" + token, BaseURL: "https://coze.test/#" + token}, "invalid_base_url"},
		{"malformed endpoint", gateway.Target{Secret: bot + "|" + token, BaseURL: "https://coze.test/%ZZ/" + token}, "invalid_base_url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (coze.Provider{}).BuildRequest(context.Background(), chatRequest(true), tc.target)
			var typed *gateway.UpstreamError
			if !errors.As(err, &typed) || typed.Status != http.StatusBadGateway || typed.Type != "upstream_error" || typed.Code != tc.code {
				t.Fatalf("channel configuration error = %+v; want HTTP 502 upstream_error %s", typed, tc.code)
			}
			if strings.Contains(err.Error(), bot) || strings.Contains(err.Error(), token) {
				t.Fatal("channel configuration error exposed a credential")
			}
		})
	}
}
