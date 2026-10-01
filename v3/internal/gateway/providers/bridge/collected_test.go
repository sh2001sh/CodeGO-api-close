package bridge

import (
	"encoding/json"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestNonstreamCallerReceivesOneJSONBodyWhenUpstreamForcesSSE(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		out := decode(t, request(protocol, false), toolStream(), nil)
		var bodies []gateway.Event
		for _, event := range out {
			if event.Kind == gateway.EventData {
				bodies = append(bodies, event)
				if event.Name != "" {
					t.Fatal("nonstream caller received an SSE lifecycle")
				}
			}
		}
		if len(bodies) != 1 || !json.Valid(bodies[0].Payload) || bodies[0].Usage == nil || bodies[0].Usage.CompletionTokens != 7 {
			t.Fatalf("invalid collected response: %#v", bodies)
		}
		payload := bodies[0].Payload
		var text, tool string
		switch protocol {
		case gateway.ProtocolResponses:
			text = "output.1.content.0.text"
			tool = "output.2.name"
		case gateway.ProtocolAnthropic:
			text = "content.1.text"
			tool = "content.2.name"
		case gateway.ProtocolGemini:
			text = "candidates.0.content.parts.1.text"
			tool = "candidates.0.content.parts.2.functionCall.name"
		}
		if gjson.GetBytes(payload, text).Str != "Hi" || gjson.GetBytes(payload, tool).Str != "first" {
			t.Fatalf("collected data lost: %s", payload)
		}
	}
}

func TestNonstreamSSEFailureDoesNotWritePartialJSON(t *testing.T) {
	failure := &gateway.UpstreamError{Status: 502, Code: "failed"}
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		out := decode(t, request(protocol, false), []gateway.Event{chunk(`{"content":"partial"}`, nil), {Kind: gateway.EventError, Err: failure}}, nil)
		for _, event := range out {
			if event.Kind == gateway.EventData || event.Kind == gateway.EventDone {
				t.Fatal("failure returned a partial success body")
			}
		}
		if out[len(out)-1].Err != failure {
			t.Fatal("upstream error lost")
		}
	}
}

func TestStreamingCallerAcceptsChatJSONWithoutMergingParallelTools(t *testing.T) {
	payload := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"first","arguments":"{\"a\":1}"}},{"id":"b","type":"function","function":{"name":"second","arguments":"{\"b\":2}"}}]},"finish_reason":"tool_calls"}]}`)
	out := decode(t, request(gateway.ProtocolResponses, true), []gateway.Event{{Kind: gateway.EventData, Payload: payload}}, nil)
	terminal := out[len(out)-2]
	if terminal.Name != "response.completed" || gjson.GetBytes(terminal.Payload, "response.output.#").Int() != 2 || gjson.GetBytes(terminal.Payload, "response.output.0.name").Str != "first" || gjson.GetBytes(terminal.Payload, "response.output.1.name").Str != "second" {
		t.Fatalf("parallel tools merged: %s", terminal.Payload)
	}
}
