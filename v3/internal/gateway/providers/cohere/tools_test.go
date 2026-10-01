package cohere

import (
	"context"
	"io"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestV2RequestPreservesToolsResultsAndSchema(t *testing.T) {
	body := `{"messages":[{"role":"developer","content":"rules"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Tokyo\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"},{"role":"user","content":"what next?"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"required","response_format":{"type":"json_schema","json_schema":{"name":"reply","schema":{"type":"object"}}}}`
	out, err := (Provider{}).BuildRequest(context.Background(), fixtureRequest(body, true), gateway.Target{BaseURL: "https://example.invalid/v2/", Secret: "test"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"messages.0.role": "system", "messages.1.tool_calls.0.id": "call_1", "messages.1.tool_calls.0.function.arguments": `{"city":"Tokyo"}`, "messages.2.tool_call_id": "call_1", "messages.2.content": "sunny", "tool_choice": "REQUIRED", "tools.0.function.name": "lookup", "response_format.schema.type": "object"} {
		if got := gjson.GetBytes(data, path).String(); got != want {
			t.Fatalf("%s = %q, want %q; body=%s", path, got, want, data)
		}
	}
}

func TestV2SingleToolCallIsNotAnEmptyResponse(t *testing.T) {
	s := fixtureStream(false, `{"id":"r1","message":{"role":"assistant","content":[],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Tokyo\"}"}}]},"finish_reason":"TOOL_CALL","usage":{"billed_units":{"input_tokens":4,"output_tokens":2}}}`, "application/json", false)
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventData || ev.Usage == nil || ev.Usage.PromptTokens != 4 || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str != "tool_calls" || gjson.GetBytes(ev.Payload, "choices.0.message.tool_calls.0.function.arguments").Str != `{"city":"Tokyo"}` || ev.TextBytes != len(`{"city":"Tokyo"}`) {
		t.Fatalf("tool completion=%+v %s %v", ev, ev.Payload, err)
	}
}

func TestV2ToolStreamPreservesIndexesArgumentFragmentsAndUsage(t *testing.T) {
	data := event("message-start", `{"type":"message-start","id":"r1"}`) +
		event("tool-call-start", `{"type":"tool-call-start","index":2,"delta":{"message":{"tool_calls":{"id":"call_1","type":"function","function":{"name":"lookup","arguments":""}}}}}`) +
		event("tool-call-delta", `{"type":"tool-call-delta","index":2,"delta":{"message":{"tool_calls":{"function":{"arguments":"{\"city\":"}}}}}`) +
		event("tool-call-delta", `{"type":"tool-call-delta","index":2,"delta":{"message":{"tool_calls":{"function":{"arguments":"\"Tokyo\"}"}}}}}`) +
		event("tool-call-end", `{"type":"tool-call-end","index":2}`) +
		event("message-end", `{"type":"message-end","delta":{"finish_reason":"TOOL_CALL","usage":{"billed_units":{"input_tokens":8,"output_tokens":3}}}}`)
	s := fixtureStream(true, data, "text/event-stream", true)
	defer func() { _ = s.Close() }()
	for i, want := range []string{"", `{"city":`, `"Tokyo"}`} {
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventData || gjson.GetBytes(ev.Payload, "choices.0.delta.tool_calls.0.index").Int() != 2 || gjson.GetBytes(ev.Payload, "choices.0.delta.tool_calls.0.function.arguments").Str != want || ev.TextBytes != len(want) {
			t.Fatalf("fragment %d=%+v %s %v", i, ev, ev.Payload, err)
		}
	}
	ev, err := s.Next()
	if err != nil || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str != "tool_calls" {
		t.Fatalf("finish=%+v %v", ev, err)
	}
	ev, err = s.Next()
	if err != nil || ev.Usage == nil || ev.Usage.CompletionTokens != 3 || ev.Kind != gateway.EventData || gjson.GetBytes(ev.Payload, "choices.#").Int() != 0 {
		t.Fatalf("usage=%+v %v", ev, err)
	}
	if ev, err = s.Next(); err != nil || ev.Kind != gateway.EventDone {
		t.Fatalf("done=%+v %v", ev, err)
	}
}

func TestV2MalformedOrOutOfOrderToolsFail(t *testing.T) {
	for _, data := range []string{
		event("tool-call-delta", `{"type":"tool-call-delta","index":0,"delta":{"message":{"tool_calls":{"function":{"arguments":"{}"}}}}}`),
		event("tool-call-start", `{"type":"tool-call-start","index":0,"delta":{"message":{"tool_calls":{"id":"c","function":{"arguments":""}}}}}`),
		event("message-start", `{"type":"message-start"}`) + event("error", `{"type":"error","message":"failed"}`),
	} {
		s := fixtureStream(true, data, "text/event-stream", false)
		ev, err := s.Next()
		_ = s.Close()
		if err == nil && ev.Kind != gateway.EventError {
			t.Fatalf("bad event committed output=%+v %v", ev, err)
		}
	}
}
