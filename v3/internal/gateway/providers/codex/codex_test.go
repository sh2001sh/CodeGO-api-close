package codex_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/codex"
)

func TestCodexHeadersBodyAndURL(t *testing.T) {
	for _, base := range []string{"https://chatgpt.example", "https://chatgpt.example/backend-api", "https://chatgpt.example/backend-api/codex", "https://chatgpt.example/backend-api/codex/responses"} {
		original := []byte(`{"model":"public","input":"hi","store":true,"max_output_tokens":20,"temperature":0.5,"frequency_penalty":1,"presence_penalty":1,"top_p":0.9,"previous_response_id":"resp_previous","tools":[{"type":"function","name":"lookup"}]}`)
		r, err := (codex.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolResponses, Body: original, Model: "public", Stream: true}, gateway.Target{BaseURL: base, UpstreamModel: "gpt-codex", Secret: `{"access_token":" test-token ","account_id":"test-account"}`})
		if err != nil {
			t.Fatal(err)
		}
		if r.URL.String() != "https://chatgpt.example/backend-api/codex/responses" || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Chatgpt-Account-Id") != "test-account" || r.Header.Get("Openai-Beta") != "responses=experimental" || r.Header.Get("Originator") != "codex_cli_rs" {
			t.Fatalf("request = %s %v", r.URL, r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["store"] != false || body["model"] != "gpt-codex" || body["instructions"] != "" || body["previous_response_id"] != "resp_previous" || body["tools"] == nil {
			t.Fatalf("body = %#v", body)
		}
		for _, field := range []string{"max_output_tokens", "temperature", "frequency_penalty", "presence_penalty", "top_p"} {
			if _, ok := body[field]; ok {
				t.Fatalf("unsupported field %s preserved", field)
			}
		}
		if !strings.Contains(string(original), `"store":true`) {
			t.Fatal("original body mutated")
		}
	}
}

func TestCodexRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		protocol        gateway.Protocol
		key, body, base string
	}{
		{gateway.ProtocolAnthropic, `{"access_token":"test","account_id":"a"}`, `{}`, "https://chatgpt.example"},
		{gateway.ProtocolResponses, "plain-key", `{}`, "https://chatgpt.example"},
		{gateway.ProtocolResponses, `{"access_token":"test"}`, `{}`, "https://chatgpt.example"},
		{gateway.ProtocolResponses, `{"access_token":"test","account_id":"a"}`, `[]`, "https://chatgpt.example"},
		{gateway.ProtocolResponses, `{"access_token":"test","account_id":"a"}`, `{}`, "chatgpt.example"},
	} {
		_, err := (codex.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: tc.protocol, Body: []byte(tc.body)}, gateway.Target{BaseURL: tc.base, Secret: tc.key})
		if err == nil {
			t.Fatalf("invalid input accepted: %+v", tc)
		}
		if strings.Contains(err.Error(), tc.key) {
			t.Fatal("error exposes credential")
		}
	}
}

func TestCodexChatDelegatesResponsesConversion(t *testing.T) {
	req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "public", Body: []byte(`{"model":"public","messages":[{"role":"user","content":"hello"}],"max_tokens":30}`)}
	out, err := (codex.Provider{}).BuildRequest(context.Background(), req, gateway.Target{BaseURL: "https://chatgpt.example", UpstreamModel: "actual", Secret: `{"access_token":"test","account_id":"a"}`})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.NewDecoder(out.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "actual" || body["store"] != false || body["instructions"] != "" || body["messages"] != nil || body["input"] == nil || body["max_output_tokens"] != nil {
		t.Fatalf("body=%#v", body)
	}
	s := (codex.Provider{}).Decode(req, &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":7,"output_tokens":2}}`))})
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventData || ev.Usage == nil || ev.Usage.PromptTokens != 7 || !strings.Contains(string(ev.Payload), `"object":"chat.completion"`) || !strings.Contains(string(ev.Payload), `"content":"hello"`) {
		t.Fatalf("Chat response=%+v %s %v", ev, ev.Payload, err)
	}
}

func TestCodexStreamTerminalUsageAndTruncation(t *testing.T) {
	prefix := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"
	terminal := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":7,\"output_tokens\":2,\"input_tokens_details\":{\"cached_tokens\":3}}}}\n\n"
	for _, end := range []string{terminal, ""} {
		s := (codex.Provider{}).Decode(&gateway.Request{Protocol: gateway.ProtocolResponses, Stream: true}, &http.Response{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(prefix + end))})
		if _, err := s.Next(); err != nil {
			t.Fatal(err)
		}
		ev, err := s.Next()
		if end == "" {
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("truncated stream = %v", err)
			}
		} else if err != nil || ev.Usage == nil || ev.Usage.CachedTokens != 3 || ev.Usage.CompletionTokens != 2 {
			t.Fatalf("event = %+v %v", ev, err)
		}
		_ = s.Close()
	}
}
