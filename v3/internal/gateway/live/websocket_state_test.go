package live

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func decodeStateBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func normalizeStateTurn(t *testing.T, state *responseState, payload string) *responseTurn {
	t.Helper()
	turn, err := state.normalize([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	return turn
}

func TestResponseStateRejectsInvalidRequests(t *testing.T) {
	for _, tt := range []struct {
		name, payload, message string
	}{
		{"malformed", `{"type":"response.create"`, "invalid websocket request JSON"},
		{"array event", `[]`, "invalid websocket request JSON"},
		{"unsupported", `{"type":"response.cancel","model":"gpt-5"}`, "unsupported websocket request type"},
		{"no event type", `{"model":"gpt-5"}`, "unsupported websocket request type"},
		{"missing model", `{"type":"response.create"}`, "missing model"},
		{"blank model", `{"type":"response.create","model":"  "}`, "missing model"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := &responseState{}
			turn, err := state.normalize([]byte(tt.payload))
			if err == nil || !strings.Contains(err.Error(), tt.message) || turn != nil {
				t.Fatalf("turn=%+v err=%v; want error containing %q", turn, err, tt.message)
			}
		})
	}
}

func TestResponseStateNormalizesCreate(t *testing.T) {
	turn := normalizeStateTurn(t, &responseState{}, `{"type":"response.create","model":"gpt-5","generate":true,"background":true,"stream":false,"store":false,"tools":[{"type":"web_search"}]}`)
	got := decodeStateBody(t, turn.body)
	want := decodeStateBody(t, []byte(`{"model":"gpt-5","stream":true,"store":false,"tools":[{"type":"web_search"}],"input":[]}`))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body=%s; want %+v", turn.body, want)
	}
	if !turn.canMerge || turn.referencedCached || len(turn.prewarmPayloads) != 0 {
		t.Fatalf("unexpected turn flags: %+v", turn)
	}
}

func TestResponseStateMergesCachedConversation(t *testing.T) {
	for _, kind := range []string{"response.create", "response.append"} {
		t.Run(kind, func(t *testing.T) {
			state := &responseState{}
			first := normalizeStateTurn(t, state, `{"type":"response.create","model":"gpt-5","input":"hello","instructions":"brief","store":false,"temperature":0.7}`)
			state.complete(first, "resp_1", []byte(`[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]`))
			second := normalizeStateTurn(t, state, `{"type":"`+kind+`","previous_response_id":"resp_1","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}],"temperature":0.1}`)
			got := decodeStateBody(t, second.body)
			want := decodeStateBody(t, []byte(`{"model":"gpt-5","stream":true,"instructions":"brief","store":false,"temperature":0.1,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("body=%s; want %+v", second.body, want)
			}
			if !second.canMerge || !second.referencedCached {
				t.Fatalf("unexpected continuation flags: %+v", second)
			}
		})
	}
}

func TestResponseStateAppendInheritsModelButAllowsOverride(t *testing.T) {
	for _, tt := range []struct{ model, want string }{
		{"", "gpt-5"},
		{`,"model":"gpt-5-mini"`, "gpt-5-mini"},
	} {
		state := &responseState{}
		first := normalizeStateTurn(t, state, `{"type":"response.create","model":"gpt-5","input":[]}`)
		state.complete(first, "resp_1", nil)
		second := normalizeStateTurn(t, state, `{"type":"response.append","input":"continue"`+tt.model+`}`)
		if got := decodeStateBody(t, second.body)["model"]; got != tt.want {
			t.Fatalf("model=%v; want %s", got, tt.want)
		}
		if !second.referencedCached || !second.canMerge {
			t.Fatalf("unexpected append flags: %+v", second)
		}
	}
}

func TestResponseStateExternalContinuationRemainsUpstream(t *testing.T) {
	state := &responseState{}
	turn := normalizeStateTurn(t, state, `{"type":"response.create","model":"gpt-5","previous_response_id":"resp_external","store":true,"input":"question"}`)
	got := decodeStateBody(t, turn.body)
	if got["previous_response_id"] != "resp_external" || got["input"] != "question" || turn.canMerge || turn.referencedCached {
		t.Fatalf("external continuation rewritten: body=%s flags=%+v", turn.body, turn)
	}
	state.complete(turn, "resp_2", []byte(`[]`))
	// The upstream-only history cannot safely be reconstructed as local input.
	next := normalizeStateTurn(t, state, `{"type":"response.create","previous_response_id":"resp_2","input":[]}`)
	if got := decodeStateBody(t, next.body); got["model"] != "gpt-5" || got["previous_response_id"] != "resp_2" || next.canMerge || !next.referencedCached {
		t.Fatalf("cached upstream-only continuation rewritten: %s", next.body)
	}
	_, err := state.normalize([]byte(`{"type":"response.append","input":[]}`))
	var protocolError *responseProtocolError
	if !errors.As(err, &protocolError) || protocolError.status != 400 || protocolError.code != "previous_response_not_found" || protocolError.param != "previous_response_id" {
		t.Fatalf("append error=%#v; want structured unavailable-history error", err)
	}
}

func TestResponseStateAppendRequiresAvailableHistory(t *testing.T) {
	_, err := (&responseState{}).normalize([]byte(`{"type":"response.append","model":"gpt-5","input":[]}`))
	var protocolError *responseProtocolError
	if !errors.As(err, &protocolError) || protocolError.status != 400 || protocolError.code != "previous_response_not_found" || protocolError.param != "previous_response_id" || protocolError.message == "" {
		t.Fatalf("append error=%#v; want structured unavailable-history error", err)
	}
}

func TestResponseStateMergesStringArrayAndNullInput(t *testing.T) {
	for _, tt := range []struct{ name, first, output, next, want string }{
		{"strings", `"hello"`, `[]`, `"next"`, `[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}]`},
		{"empty", `null`, `null`, `[]`, `[]`},
		{"tool output", `[]`, `[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}]`, `[{"type":"function_call_output","call_id":"call_1","output":"done"}]`, `[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"done"}]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := &responseState{}
			first := normalizeStateTurn(t, state, `{"type":"response.create","model":"gpt-5","input":`+tt.first+`}`)
			state.complete(first, "resp_1", []byte(tt.output))
			next := normalizeStateTurn(t, state, `{"type":"response.append","input":`+tt.next+`}`)
			var want any
			if err := json.Unmarshal([]byte(tt.want), &want); err != nil {
				t.Fatal(err)
			}
			if got := decodeStateBody(t, next.body)["input"]; !reflect.DeepEqual(got, want) {
				t.Fatalf("input=%+v; want %+v", got, want)
			}
		})
	}
}

func TestResponseStateRejectsInvalidContinuationInput(t *testing.T) {
	for _, input := range []string{`12`, `true`, `{"text":"unsupported"}`} {
		state := &responseState{}
		first := normalizeStateTurn(t, state, `{"type":"response.create","model":"gpt-5","input":[]}`)
		state.complete(first, "resp_1", nil)
		turn, err := state.normalize([]byte(`{"type":"response.append","input":` + input + `}`))
		if err == nil || turn != nil || !strings.Contains(err.Error(), "string or array input") {
			t.Fatalf("input=%s turn=%+v err=%v", input, turn, err)
		}
		// Validation failure must leave the last completed response usable.
		if state.lastResponseID != "resp_1" {
			t.Fatal("invalid input evicted completed response")
		}
	}
}

func TestResponseStatePrewarmHasZeroUsageAndCanContinue(t *testing.T) {
	state := &responseState{}
	turn := normalizeStateTurn(t, state, `{"type":"response.create","model":"gpt-5","generate":false,"background":true,"input":"hello"}`)
	if len(turn.prewarmPayloads) != 2 {
		t.Fatalf("prewarm events=%d; want 2", len(turn.prewarmPayloads))
	}
	var responseID string
	for i, typ := range []string{"response.created", "response.completed"} {
		event := decodeStateBody(t, turn.prewarmPayloads[i])
		response := event["response"].(map[string]any)
		if event["type"] != typ || event["sequence_number"] != float64(i) || response["model"] != "gpt-5" || response["object"] != "response" || response["background"] != false || response["error"] != nil || len(response["output"].([]any)) != 0 {
			t.Fatalf("invalid prewarm event: %s", turn.prewarmPayloads[i])
		}
		if i == 0 {
			responseID = response["id"].(string)
			if !strings.HasPrefix(responseID, "resp_prewarm_") || response["status"] != "in_progress" {
				t.Fatalf("invalid created response: %+v", response)
			}
		} else {
			wantUsage := map[string]any{"input_tokens": float64(0), "output_tokens": float64(0), "total_tokens": float64(0)}
			if response["id"] != responseID || response["status"] != "completed" || !reflect.DeepEqual(response["usage"], wantUsage) {
				t.Fatalf("invalid completed response: %+v", response)
			}
		}
	}
	if _, ok := decodeStateBody(t, turn.body)["generate"]; ok {
		t.Fatal("generate leaked into upstream body")
	}
	state.complete(turn, responseID, nil)
	next := normalizeStateTurn(t, state, `{"type":"response.create","previous_response_id":"`+responseID+`","input":"next"}`)
	if got := decodeStateBody(t, next.body); got["model"] != "gpt-5" || len(got["input"].([]any)) != 2 {
		t.Fatalf("prewarm continuation not chainable: %s", next.body)
	}
}

func TestResponseStateFailOnlyEvictsReferencedHistory(t *testing.T) {
	state := &responseState{}
	first := normalizeStateTurn(t, state, `{"type":"response.create","model":"gpt-5","input":[]}`)
	state.complete(first, "resp_1", nil)
	state.fail(nil)
	unrelated := normalizeStateTurn(t, state, `{"type":"response.create","model":"gpt-5","input":"new"}`)
	state.fail(unrelated)
	if state.lastResponseID != "resp_1" {
		t.Fatal("unrelated failure evicted completed response")
	}
	continuation := normalizeStateTurn(t, state, `{"type":"response.append","input":[]}`)
	state.fail(continuation)
	if len(state.lastRequest) != 0 || len(state.lastOutput) != 0 || state.lastResponseID != "" || state.canMerge {
		t.Fatalf("failed continuation retained history: %+v", state)
	}
	if _, err := state.normalize([]byte(`{"type":"response.append","input":[]}`)); err == nil {
		t.Fatal("append succeeded after history invalidation")
	}
}

func TestResponseStateCompleteOwnsBodyAndOutput(t *testing.T) {
	state := &responseState{}
	first := normalizeStateTurn(t, state, `{"type":"response.create","model":"gpt-5","input":"hello"}`)
	output := []byte(`[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]`)
	state.complete(first, "resp_1", output)
	for i := range first.body {
		first.body[i] = 'x'
	}
	for i := range output {
		output[i] = 'x'
	}
	next := normalizeStateTurn(t, state, `{"type":"response.append","input":[]}`)
	if got := decodeStateBody(t, next.body); got["model"] != "gpt-5" || len(got["input"].([]any)) != 2 {
		t.Fatalf("cached state aliases caller buffers: %s", next.body)
	}
	state.complete(nil, "ignored", nil)
	state.complete(next, "", nil)
	if state.lastResponseID != "resp_1" {
		t.Fatal("incomplete result replaced cached response")
	}
}
