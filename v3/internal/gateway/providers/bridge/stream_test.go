package bridge

import (
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func toolStream() []gateway.Event {
	return []gateway.Event{
		chunk(`{"role":"assistant"}`, nil),
		chunk(`{"reasoning_content":"think"}`, nil),
		chunk(`{"content":"Hi"}`, nil),
		chunk(`{"tool_calls":[{"index":1,"id":"b","type":"function","function":{"name":"second","arguments":"{\"b\":"}},{"index":0,"id":"a","type":"function","function":{"name":"first","arguments":"{\"a\":"}}]}`, nil),
		chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"1}"}},{"index":1,"function":{"arguments":"2}"}}]}`, nil),
		{Kind: gateway.EventUsage, Usage: &gateway.Usage{PromptTokens: 12, CompletionTokens: 7, CachedTokens: 3}},
		{Kind: gateway.EventDone},
	}
}

func TestResponsesLifecyclePairsIndicesAndLastCompletion(t *testing.T) {
	out := decode(t, request(gateway.ProtocolResponses, true), toolStream(), nil)
	sequence := int64(0)
	completed := 0
	added := map[int64]string{}
	done := map[int64]string{}
	partsAdded := 0
	partsDone := 0
	argumentDeltas := map[string]string{}
	var terminal gateway.Event
	for _, event := range out {
		if event.Kind != gateway.EventData {
			continue
		}
		root := gjson.ParseBytes(event.Payload)
		if got := root.Get("sequence_number").Int(); got != sequence {
			t.Fatalf("sequence=%d want %d", got, sequence)
		}
		sequence++
		if root.Get("type").Str != event.Name {
			t.Fatalf("SSE name differs from payload: %s", event.Payload)
		}
		switch event.Name {
		case "response.output_item.added":
			index := root.Get("output_index").Int()
			if _, ok := added[index]; ok {
				t.Fatalf("duplicate index %d", index)
			}
			added[index] = root.Get("item.id").Str
			if root.Get("item.type").Str == "function_call" && !root.Get("item.arguments").Exists() {
				t.Fatal("function item omitted initial arguments")
			}
		case "response.output_item.done":
			done[root.Get("output_index").Int()] = root.Get("item.id").Str
		case "response.content_part.added", "response.reasoning_summary_part.added":
			partsAdded++
			if !root.Get("part.text").Exists() {
				t.Fatal("initial text field omitted")
			}
		case "response.content_part.done", "response.reasoning_summary_part.done":
			partsDone++
		case "response.function_call_arguments.delta":
			argumentDeltas[root.Get("item_id").Str] += root.Get("delta").Str
		case "response.completed":
			completed++
			terminal = event
		}
	}
	if completed != 1 || out[len(out)-2].Name != "response.completed" || out[len(out)-1].Kind != gateway.EventDone {
		t.Fatal("completed is not once-last")
	}
	if len(added) != 4 || len(done) != len(added) || partsAdded != 2 || partsDone != partsAdded {
		t.Fatalf("lifecycle pairs: added=%v done=%v parts=%d/%d", added, done, partsAdded, partsDone)
	}
	for index, id := range added {
		if done[index] != id {
			t.Fatalf("index %d item mismatch", index)
		}
	}
	if argumentDeltas["resp_test_tool_0"] != `{"a":1}` || argumentDeltas["resp_test_tool_1"] != `{"b":2}` {
		t.Fatalf("interleaved arguments mixed: %v", argumentDeltas)
	}
	root := gjson.ParseBytes(terminal.Payload)
	if root.Get("response.output.2.call_id").Str != "b" || root.Get("response.output.3.call_id").Str != "a" {
		t.Fatal("output indices did not follow emission order")
	}
	if terminal.Usage == nil || terminal.Usage.PromptTokens != 12 || root.Get("response.usage.input_tokens_details.cached_tokens").Int() != 3 {
		t.Fatal("final usage lost")
	}
}

func TestAnthropicBlocksCloseBeforeNextBlockAndStop(t *testing.T) {
	out := decode(t, request(gateway.ProtocolAnthropic, true), toolStream(), nil)
	open := false
	index := int64(-1)
	starts := 0
	stops := 0
	start := 0
	stop := 0
	args := map[int64]string{}
	var terminal gateway.Event
	for _, event := range out {
		if event.Kind != gateway.EventData {
			continue
		}
		root := gjson.ParseBytes(event.Payload)
		switch event.Name {
		case "message_start":
			start++
		case "content_block_start":
			if open {
				t.Fatal("new block started before prior block stopped")
			}
			open = true
			index++
			if root.Get("index").Int() != index {
				t.Fatal("non-contiguous content index")
			}
			starts++
		case "content_block_delta":
			if !open || root.Get("index").Int() != index {
				t.Fatal("delta outside its block")
			}
			if root.Get("delta.type").Str == "input_json_delta" {
				args[index] += root.Get("delta.partial_json").Str
			}
		case "content_block_stop":
			if !open {
				t.Fatal("duplicate block stop")
			}
			open = false
			stops++
		case "message_delta":
			if open {
				t.Fatal("message delta before block stop")
			}
			terminal = event
		case "message_stop":
			stop++
		}
	}
	if open || starts != 4 || stops != starts || start != 1 || stop != 1 {
		t.Fatalf("invalid lifecycle %d/%d %d/%d", starts, stops, start, stop)
	}
	if args[2] != `{"a":1}` || args[3] != `{"b":2}` {
		t.Fatalf("tool arguments mixed: %v", args)
	}
	if terminal.Usage == nil || gjson.GetBytes(terminal.Payload, "delta.stop_reason").Str != "tool_use" || gjson.GetBytes(terminal.Payload, "usage.output_tokens").Int() != 7 {
		t.Fatal("Anthropic terminal usage or stop reason lost")
	}
}

func TestGeminiSSEKeepsThoughtTextToolArgumentsAndFinalUsage(t *testing.T) {
	out := decode(t, request(gateway.ProtocolGemini, true), toolStream(), nil)
	var data []gateway.Event
	for _, event := range out {
		if event.Kind == gateway.EventData {
			data = append(data, event)
			if event.Name != "" {
				t.Fatal("Gemini invented an SSE event name")
			}
		}
	}
	if len(data) != 3 {
		t.Fatalf("data frames=%d want 3", len(data))
	}
	if !gjson.GetBytes(data[0].Payload, "candidates.0.content.parts.0.thought").Bool() || gjson.GetBytes(data[0].Payload, "candidates.0.content.parts.0.text").Str != "think" {
		t.Fatal("reasoning lost")
	}
	if gjson.GetBytes(data[1].Payload, "candidates.0.content.parts.0.text").Str != "Hi" {
		t.Fatal("text lost")
	}
	final := data[2]
	if final.Usage == nil || gjson.GetBytes(final.Payload, "candidates.0.finishReason").Str != "STOP" || gjson.GetBytes(final.Payload, "usageMetadata.totalTokenCount").Int() != 19 {
		t.Fatal("usage or stop lost")
	}
	if gjson.GetBytes(final.Payload, "candidates.0.content.parts.0.functionCall.args.a").Int() != 1 || gjson.GetBytes(final.Payload, "candidates.0.content.parts.1.functionCall.args.b").Int() != 2 {
		t.Fatal("tool args mixed")
	}
}

func TestEmptyOrRoleOnlyChatStreamsRemainEmptyForFailover(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		out := decode(t, request(protocol, true), []gateway.Event{chunk(`{"role":"assistant"}`, nil), {Kind: gateway.EventDone}}, nil)
		for _, event := range out {
			if event.Kind == gateway.EventData {
				t.Fatal("empty upstream became a successful native response")
			}
		}
	}
}

func TestInvalidCompletedToolArgumentsFailBeforeCompletion(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		events := []gateway.Event{chunk(`{"tool_calls":[{"index":0,"id":"a","function":{"name":"first","arguments":"{broken"}}]}`, nil), {Kind: gateway.EventDone}}
		stream := (Provider{Chat: &fakeProvider{source: &fakeStream{events: events}}}).Decode(request(protocol, true), &http.Response{})
		for {
			event, err := stream.Next()
			if err != nil {
				if !strings.Contains(err.Error(), "tool-call arguments") {
					t.Fatal(err)
				}
				break
			}
			if event.Name == "response.completed" || event.Name == "message_stop" || event.Kind == gateway.EventDone {
				t.Fatal("malformed tool completed")
			}
		}
	}
}

func TestResponsesEmitterFinishIsIdempotent(t *testing.T) {
	emitter := NewResponsesEmitter("r", "m")
	if _, err := emitter.OnEvent(chunk(`{"content":"x"}`, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := emitter.Finish(nil, "stop"); err != nil {
		t.Fatal(err)
	}
	events, err := emitter.Finish(nil, "stop")
	if err != nil || len(events) != 0 {
		t.Fatal("duplicate completion")
	}
	if _, err = emitter.OnEvent(chunk(`{"content":"late"}`, nil)); err == nil {
		t.Fatal("late output accepted")
	}
}

func TestTruncatedOutputUsesNativeIncompleteReason(t *testing.T) {
	events := []gateway.Event{chunk(`{"content":"partial"}`, nil), {Kind: gateway.EventData, Payload: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`)}, {Kind: gateway.EventDone}}
	out := decode(t, request(gateway.ProtocolResponses, true), events, nil)
	terminal := out[len(out)-2]
	if terminal.Name != "response.incomplete" || gjson.GetBytes(terminal.Payload, "response.incomplete_details.reason").Str != "max_output_tokens" {
		t.Fatal("truncation incorrectly reported complete")
	}
}
