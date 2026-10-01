package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestNativeRequest(t *testing.T) {
	data := []byte(`{"model":"public","stream":true,"messages":[{"role":"user","content":"hello"}],"max_tokens":256,"thinking":{"type":"enabled","budget_tokens":100}}`)
	for _, base := range []string{"https://example.test", "https://example.test/v1/"} {
		req := &gateway.Request{Protocol: gateway.ProtocolAnthropic, Body: data, Model: "public", Stream: true}
		up, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{BaseURL: base, Secret: "test-secret", UpstreamModel: "private"})
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(up.Body)
		if err != nil {
			t.Fatal(err)
		}
		if up.URL.String() != "https://example.test/v1/messages" || up.Header.Get("X-Api-Key") != "test-secret" || up.Header.Get("Anthropic-Version") != "2023-06-01" || up.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("wrong upstream request: %s %v", up.URL, up.Header)
		}
		if gjson.GetBytes(body, "model").Str != "private" || gjson.GetBytes(body, "thinking.budget_tokens").Int() != 100 {
			t.Fatalf("lost native fields: %s", body)
		}
		if gjson.GetBytes(data, "model").Str != "public" {
			t.Fatal("mutated caller body")
		}
	}
}

func TestChatRequestToolsImagesAndSystem(t *testing.T) {
	data := []byte(`{"model":"public","stream":true,"max_completion_tokens":250,"temperature":0,"top_p":0.9,"stop":"END","user":"u1","parallel_tool_calls":false,"tool_choice":{"type":"function","function":{"name":"weather"}},"tools":[{"type":"function","function":{"name":"weather","description":"lookup","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],"messages":[{"role":"system","content":"policy"},{"role":"developer","content":"format"},{"role":"user","content":[{"type":"text","text":"weather?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGk="}}]},{"role":"assistant","content":"","tool_calls":[{"id":"call1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Paris\"}"}}]},{"role":"tool","tool_call_id":"call1","content":"sunny"},{"role":"tool","tool_call_id":"call2","content":"warm"}]}`)
	body, err := convertRequest(data, "mapped")
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{"model": "mapped", "system.0.text": "policy", "system.1.text": "format",
		"messages.0.content.1.source.type": "base64", "messages.0.content.1.source.media_type": "image/png",
		"messages.1.content.0.type": "tool_use", "messages.1.content.0.input.city": "Paris",
		"messages.2.content.0.tool_use_id": "call1", "messages.2.content.1.tool_use_id": "call2",
		"tools.0.input_schema.properties.city.type": "string", "tool_choice.type": "tool", "tool_choice.name": "weather", "stop_sequences.0": "END", "metadata.user_id": "u1"}
	for path, want := range checks {
		if got := gjson.GetBytes(body, path).Str; got != want {
			t.Errorf("%s=%q, want %q (%s)", path, got, want, body)
		}
	}
	if gjson.GetBytes(body, "messages.#").Int() != 3 || !gjson.GetBytes(body, "tool_choice.disable_parallel_tool_use").Bool() || gjson.GetBytes(body, "max_tokens").Int() != 250 || gjson.GetBytes(body, "temperature").Float() != 0 {
		t.Fatalf("wrong translated request: %s", body)
	}
}

func TestRejectLossyChatRequests(t *testing.T) {
	base := `"model":"m","messages":[{"role":"user","content":"hello"}]`
	for _, extra := range []string{`"n":2`, `"seed":1`, `"presence_penalty":0.5`, `"response_format":{"type":"json_object"}`, `"modalities":["audio"]`, `"stream_options":{"unknown":true}`, `"tools":[{"type":"function","function":{"name":"x","strict":true}}]`, `"max_tokens":0`} {
		t.Run(extra, func(t *testing.T) {
			req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Body: []byte("{" + base + "," + extra + "}"), Model: "m"}
			_, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{BaseURL: "https://example.test"})
			var clientErr *gateway.UpstreamError
			if !errors.As(err, &clientErr) || clientErr.Status != http.StatusBadRequest || clientErr.Code != "unsupported_request" {
				t.Fatalf("expected client request error, got %v", err)
			}
		})
	}
	for _, data := range []string{
		`{"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA=="}}]}]}`,
		`{"messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"x","type":"function","function":{"name":"f","arguments":"not-json"}}]}]}`,
		`{"messages":[{"role":"user","name":"ignored?","content":"hello"}]}`,
		`{"messages":[{"role":"system","content":"policy"}]}`,
		`{"messages":[{"role":"tool","content":"result"}]}`,
	} {
		if _, err := convertRequest([]byte(data), "m"); err == nil {
			t.Errorf("accepted unsupported request %s", data)
		}
	}
}

func TestImageAndToolChoiceBoundaries(t *testing.T) {
	for _, value := range []string{"https://example.test/image.png", "data:image/jpeg;base64,aGk="} {
		if _, err := convertImage(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"file:///private", "data:text/html;base64,aGk=", "data:image/png;base64,!", "data:image/png,raw"} {
		if _, err := convertImage(value); err == nil {
			t.Errorf("accepted invalid image %q", value)
		}
	}
	for _, tc := range []struct{ raw, want string }{{`"auto"`, "auto"}, {`"none"`, "none"}, {`"required"`, "any"}} {
		choice, err := convertToolChoice([]byte(tc.raw))
		if err != nil || choice.Type != tc.want {
			t.Fatalf("tool choice %s: %v %v", tc.raw, choice, err)
		}
	}
	if _, err := convertRequest([]byte(`{"messages":[{"role":"user","content":"hello"}],"stop":42}`), "m"); err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatalf("invalid stop accepted: %v", err)
	}
}
