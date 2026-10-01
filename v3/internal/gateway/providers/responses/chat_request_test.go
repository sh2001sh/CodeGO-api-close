package responses_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

func chatRequest(t *testing.T, body string) gjson.Result {
	t.Helper()
	req, err := (responses.Provider{}).BuildRequest(context.Background(), &gateway.Request{
		Protocol: gateway.ProtocolOpenAIChat, Body: []byte(body), Model: "client", Stream: true,
	}, gateway.Target{BaseURL: "https://upstream.example", Secret: "test", UpstreamModel: "actual"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = req.Body.Close() }()
	got, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/v1/responses" || req.Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("request = %+v", req)
	}
	return gjson.ParseBytes(got)
}

func TestChatRequestPreservesMessagesAndCompatibleOptions(t *testing.T) {
	root := chatRequest(t, `{"messages":[{"role":"system","content":"policy"},{"role":"developer","content":"format"},{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA","detail":"high"}}]},{"role":"assistant","content":"answer"}],"max_completion_tokens":123,"temperature":0.2,"top_p":0.8,"reasoning_effort":"high","stream_options":{"include_usage":true},"response_format":{"type":"json_schema","json_schema":{"name":"result","schema":{"type":"object"},"strict":true}}}`)
	for path, want := range map[string]string{
		"model": "actual", "input.0.role": "system", "input.0.content.0.text": "policy", "input.1.role": "developer",
		"input.2.content.1.type": "input_image", "input.2.content.1.detail": "high", "input.3.content.0.type": "output_text",
		"reasoning.effort": "high", "text.format.type": "json_schema", "text.format.name": "result",
	} {
		if got := root.Get(path).Str; got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if root.Get("max_output_tokens").Int() != 123 || !root.Get("stream").Bool() || !root.Get("text.format.strict").Bool() || root.Get("stream_options").Exists() || root.Get("temperature").Float() != 0.2 {
		t.Fatalf("converted options = %s", root.Raw)
	}
}

func TestChatToolsAndResultsRetainTheirIDsAndOrder(t *testing.T) {
	root := chatRequest(t, `{"messages":[{"role":"user","content":"weather?"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Paris\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}],"max_tokens":42,"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"weather"}},"parallel_tool_calls":false}`)
	if root.Get("input.1.type").Str != "function_call" || root.Get("input.1.call_id").Str != "call_1" || root.Get("input.1.arguments").Str != `{"city":"Paris"}` || root.Get("input.2.type").Str != "function_call_output" || root.Get("input.2.output").Str != "sunny" {
		t.Fatalf("input = %s", root.Get("input"))
	}
	if root.Get("tools.0.name").Str != "weather" || !root.Get("tools.0.strict").Exists() || root.Get("tools.0.strict").Bool() || root.Get("tool_choice.name").Str != "weather" || root.Get("max_output_tokens").Int() != 42 {
		t.Fatalf("tool options = %s", root.Raw)
	}
}

func TestChatLossyOrMalformedRequestsReturnTyped400(t *testing.T) {
	for _, body := range []string{
		`{"messages":[{"role":"user","content":"hello"}],"stop":["END"]}`,
		`{"messages":[{"role":"user","content":"hello"}],"n":2}`,
		`{"messages":[{"role":"user","content":"hello"}],"max_tokens":1,"max_completion_tokens":2}`,
		`{"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{}}]}]}`,
		`{"messages":[{"role":"tool","content":"orphan"}]}`,
		`{"messages":[{"role":"user","content":"hello","name":"alice"}]}`,
		`{"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"web_search"}]}`,
		`{"messages":[{"role":"user","content":"hello"}],"response_format":{"type":"json_schema","json_schema":{"name":"broken"}}}`,
		`{"messages":[]}`, `not json`,
	} {
		t.Run(body, func(t *testing.T) {
			_, err := (responses.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Body: []byte(body)}, gateway.Target{})
			var typed *gateway.UpstreamError
			if !errors.As(err, &typed) || typed.Status != http.StatusBadRequest || typed.Type != "invalid_request_error" {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
