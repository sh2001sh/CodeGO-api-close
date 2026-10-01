package responses_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

func decodeChat(body, contentType string, streaming, usage bool) gateway.EventStream {
	requestBody := `{}`
	if usage {
		requestBody = `{"stream_options":{"include_usage":true}}`
	}
	return (responses.Provider{}).Decode(&gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Stream: streaming, Model: "client", ID: "request", Body: []byte(requestBody)}, &http.Response{
		Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {contentType}},
	})
}

const chatResponse = `{"id":"resp_1","model":"upstream","created_at":123,"status":"completed","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking"}]},{"type":"message","content":[{"type":"output_text","text":"hello"},{"type":"refusal","refusal":"refusal"}]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}],"usage":{"input_tokens":10,"output_tokens":3,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":2}}}`

func TestChatCompleteJSONAndCollectedSSE(t *testing.T) {
	for _, contentType := range []string{"application/json", "text/event-stream"} {
		t.Run(contentType, func(t *testing.T) {
			body := chatResponse
			if contentType == "text/event-stream" {
				body = event("response.created", `{"type":"response.created","response":{"id":"resp_1"}}`) + event("response.completed", `{"type":"response.completed","response":`+chatResponse+`}`)
			}
			s := decodeChat(body, contentType, false, false)
			defer func() { _ = s.Close() }()
			ev, err := s.Next()
			root := gjson.ParseBytes(ev.Payload)
			if err != nil || ev.Kind != gateway.EventData || ev.Name != "" || root.Get("object").Str != "chat.completion" || root.Get("id").Str != "resp_1" || root.Get("created").Int() != 123 {
				t.Fatalf("response = %+v %v %s", ev, err, ev.Payload)
			}
			message := root.Get("choices.0.message")
			if message.Get("content").Str != "hello" || message.Get("reasoning_content").Str != "thinking" || message.Get("refusal").Str != "refusal" || message.Get("tool_calls.0.id").Str != "call_1" || root.Get("choices.0.finish_reason").Str != "tool_calls" {
				t.Fatalf("message = %s", root.Raw)
			}
			if root.Get("usage.total_tokens").Int() != 13 || root.Get("usage.prompt_tokens_details.cached_tokens").Int() != 4 || root.Get("usage.completion_tokens_details.reasoning_tokens").Int() != 2 || ev.Usage.CompletionTokens != 3 {
				t.Fatalf("usage = %s %+v", root.Get("usage"), ev.Usage)
			}
			if _, err := s.Next(); !errors.Is(err, io.EOF) {
				t.Fatalf("after JSON = %v", err)
			}
		})
	}
}

func TestChatStreamFiltersLifecycleAndFinishesBeforeUsage(t *testing.T) {
	for _, includeUsage := range []bool{false, true} {
		t.Run(map[bool]string{false: "hidden usage", true: "visible usage"}[includeUsage], func(t *testing.T) {
			body := event("response.created", `{"type":"response.created","response":{"id":"resp_1","model":"upstream","created_at":123}}`) +
				event("response.in_progress", `{"type":"response.in_progress"}`) +
				event("response.output_text.delta", `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"hello"}`) +
				event("response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":2}}}`)
			s := decodeChat(body, "text/event-stream", true, includeUsage)
			defer func() { _ = s.Close() }()
			ev, err := s.Next()
			root := gjson.ParseBytes(ev.Payload)
			if err != nil || ev.Name != "" || root.Get("choices.0.delta.content").Str != "hello" || root.Get("choices.0.delta.role").Str != "assistant" || root.Get("id").Str != "resp_1" || ev.TextBytes != 5 {
				t.Fatalf("first event = %+v %v %s", ev, err, ev.Payload)
			}
			finish, err := s.Next()
			if err != nil || gjson.GetBytes(finish.Payload, "choices.0.finish_reason").Str != "stop" || finish.Usage == nil || finish.Usage.CompletionTokens != 2 {
				t.Fatalf("finish = %+v %v", finish, err)
			}
			usage, err := s.Next()
			want := gateway.EventUsage
			if includeUsage {
				want = gateway.EventData
			}
			if err != nil || usage.Kind != want || usage.Usage.PromptTokens != 10 {
				t.Fatalf("usage = %+v %v", usage, err)
			}
			if includeUsage && (len(gjson.GetBytes(usage.Payload, "choices").Array()) != 0 || gjson.GetBytes(usage.Payload, "usage.total_tokens").Int() != 12) {
				t.Fatalf("usage chunk = %s", usage.Payload)
			}
			if !includeUsage && len(usage.Payload) != 0 {
				t.Fatal("unrequested usage was exposed")
			}
			if ev, err := s.Next(); err != nil || ev.Kind != gateway.EventDone {
				t.Fatalf("done = %+v %v", ev, err)
			}
		})
	}
}

func TestChatParallelToolsKeepIndexesAndDoNotRepeatArguments(t *testing.T) {
	body := event("response.reasoning_summary_text.delta", `{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"delta":"think"}`) +
		event("response.output_item.added", `{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":""}}`) +
		event("response.output_item.added", `{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"lookup","arguments":""}}`) +
		event("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"b\":2}"}`) +
		event("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"a\":"}`) +
		event("response.output_item.done", `{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"a\":1}"}}`) +
		event("response.completed", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"think"}]},{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"a\":1}"},{"type":"function_call","call_id":"call_2","name":"lookup","arguments":"{\"b\":2}"}],"usage":{"input_tokens":1,"output_tokens":4}}}`)
	s := decodeChat(body, "text/event-stream", true, false)
	defer func() { _ = s.Close() }()
	ids, args, names := map[int]string{}, map[int]string{}, map[int]string{}
	var reasoning, finish string
	for {
		ev, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind == gateway.EventDone {
			break
		}
		root := gjson.ParseBytes(ev.Payload)
		reasoning += root.Get("choices.0.delta.reasoning_content").Str
		if value := root.Get("choices.0.finish_reason").Str; value != "" {
			finish = value
		}
		for _, tool := range root.Get("choices.0.delta.tool_calls").Array() {
			index := int(tool.Get("index").Int())
			ids[index] += tool.Get("id").Str
			names[index] += tool.Get("function.name").Str
			args[index] += tool.Get("function.arguments").Str
		}
	}
	if reasoning != "think" || ids[0] != "call_1" || ids[1] != "call_2" || args[0] != `{"a":1}` || args[1] != `{"b":2}` || names[0] != "lookup" || names[1] != "lookup" || finish != "tool_calls" {
		t.Fatalf("reasoning=%s ids=%v args=%v names=%v finish=%s", reasoning, ids, args, names, finish)
	}
}

func TestChatFailureKeepsUsageAndHidesNativeErrorPayload(t *testing.T) {
	for _, prefix := range []string{
		event("response.created", `{"type":"response.created"}`),
		event("response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`),
	} {
		s := decodeChat(prefix+event("response.failed", `{"type":"response.failed","response":{"error":{"code":"server_error","message":"failed"},"usage":{"input_tokens":4,"output_tokens":2}}}`), "text/event-stream", true, false)
		if strings.Contains(prefix, "output_text") {
			if ev, err := s.Next(); err != nil || ev.Kind != gateway.EventData {
				t.Fatalf("prefix = %+v %v", ev, err)
			}
		}
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventError || ev.Err.Code != "server_error" || ev.Name != "" || len(ev.Payload) != 0 || ev.Usage == nil || ev.Usage.PromptTokens != 4 || ev.Usage.CompletionTokens != 2 {
			t.Fatalf("error = %+v %v", ev, err)
		}
		_ = s.Close()
	}
}

func TestChatEmptyAndUnsupportedResponsesRetainUsage(t *testing.T) {
	for _, output := range []string{`[]`, `[{"type":"web_search_call","status":"completed"}]`} {
		for _, contentType := range []string{"application/json", "text/event-stream"} {
			body := `{"status":"completed","output":` + output + `,"usage":{"input_tokens":3,"output_tokens":1}}`
			if contentType == "text/event-stream" {
				body = event("response.completed", `{"type":"response.completed","response":`+body+`}`)
			}
			s := decodeChat(body, contentType, true, false)
			ev, err := s.Next()
			if err != nil || ev.Kind != gateway.EventError || ev.Usage == nil || ev.Usage.PromptTokens != 3 {
				t.Fatalf("body %s error = %+v %v", body, ev, err)
			}
			_ = s.Close()
		}
	}
}

func TestChatIncompleteAndInterruptedStreams(t *testing.T) {
	for _, reason := range []string{"max_output_tokens", "content_filter"} {
		body := event("response.refusal.delta", `{"type":"response.refusal.delta","output_index":0,"content_index":0,"delta":"cannot"}`) +
			event("response.incomplete", `{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"`+reason+`"},"output":[{"type":"message","content":[{"type":"refusal","refusal":"cannot"}]}],"usage":{"input_tokens":1,"output_tokens":2}}}`)
		s := decodeChat(body, "text/event-stream", true, false)
		ev, err := s.Next()
		if err != nil || gjson.GetBytes(ev.Payload, "choices.0.delta.refusal").Str != "cannot" {
			t.Fatalf("refusal = %+v %v", ev, err)
		}
		ev, err = s.Next()
		want := "length"
		if reason == "content_filter" {
			want = "content_filter"
		}
		if err != nil || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str != want {
			t.Fatalf("finish = %s %v, want %s", ev.Payload, err, want)
		}
		_ = s.Close()
	}
	s := decodeChat(event("response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`), "text/event-stream", true, false)
	defer func() { _ = s.Close() }()
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Next(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("cut stream error = %v", err)
	}
}

func TestChatStreamingClientCanConsumeJSONUpstream(t *testing.T) {
	s := decodeChat(`{"id":"resp_1","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":2}}`, "application/json", true, true)
	defer func() { _ = s.Close() }()
	for i, kind := range []gateway.EventKind{gateway.EventData, gateway.EventData, gateway.EventData, gateway.EventDone} {
		ev, err := s.Next()
		if err != nil || ev.Kind != kind {
			t.Fatalf("event %d = %+v %v", i, ev, err)
		}
		if i == 0 && gjson.GetBytes(ev.Payload, "choices.0.delta.content").Str != "hello" {
			t.Fatalf("first chunk = %s", ev.Payload)
		}
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("after done = %v", err)
	}
}
