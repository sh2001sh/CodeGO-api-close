package ollama_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/ollama"
	"github.com/tidwall/gjson"
)

func TestNativeChatRequestConversion(t *testing.T) {
	body := []byte(`{
		"model":"client", "stream":true, "max_tokens":20, "max_completion_tokens":30,
		"temperature":0, "top_p":0.8, "top_k":10, "seed":2, "stop":"END", "think":true,
		"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}},
		"messages":[
			{"role":"developer","content":"Be brief"},
			{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aW1hZ2U="}}]},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Tokyo\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"sunny"}
		],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"stream_options":{"include_usage":true}
	}`)
	for _, base := range []string{"https://ollama.example", "https://ollama.example/api/", "https://ollama.example/v1/"} {
		request, err := (ollama.Provider{}).BuildRequest(context.Background(), &gateway.Request{
			Protocol: gateway.ProtocolOpenAIChat, Model: "client", Stream: true, Body: body,
		}, gateway.Target{BaseURL: base, Secret: "test", UpstreamModel: "upstream"})
		if err != nil {
			t.Fatal(err)
		}
		converted, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if request.URL.String() != "https://ollama.example/api/chat" || request.Header.Get("Authorization") != "Bearer test" || request.Header.Get("Accept") != "application/x-ndjson" {
			t.Fatalf("request = %s %v", request.URL, request.Header)
		}
		for path, want := range map[string]string{
			"model": "upstream", "options.num_predict": "30", "options.temperature": "0",
			"options.stop.0": "END", "messages.0.role": "system", "messages.1.content": "hi",
			"messages.1.images.0": "aW1hZ2U=", "messages.2.tool_calls.0.function.arguments.city": "Tokyo",
			"messages.3.tool_name": "lookup", "format.type": "object", "think": "true",
		} {
			if got := gjson.GetBytes(converted, path).String(); got != want {
				t.Fatalf("%s = %q, want %q; body = %s", path, got, want, converted)
			}
		}
		if gjson.GetBytes(converted, "stream_options").Exists() || !gjson.GetBytes(converted, "stream").Bool() {
			t.Fatalf("native body = %s", converted)
		}
	}
	if gjson.GetBytes(body, "model").String() != "client" {
		t.Fatal("original body changed")
	}
}

func TestRequestRejectsUnsupportedInsteadOfDroppingFields(t *testing.T) {
	for _, body := range []string{
		`not JSON`,
		`{"model":"m","messages":[]}`,
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"n":2}`,
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"logprobs":true}`,
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"tool_choice":"required"}`,
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`,
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"stop":123}`,
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema"}}`,
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"max_tokens":0}`,
		`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}]}`,
		`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,!!!"}}]}]}`,
		`{"model":"m","messages":[{"role":"tool","content":"hi","tool_call_id":"missing"}]}`,
		`{"model":"m","messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"[]"}}]}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			_, err := (ollama.Provider{}).BuildRequest(context.Background(), &gateway.Request{
				Protocol: gateway.ProtocolOpenAIChat, Model: "m", Body: []byte(body),
			}, gateway.Target{BaseURL: "https://ollama.example"})
			var upstream *gateway.UpstreamError
			if !errors.As(err, &upstream) || upstream.Status != http.StatusBadRequest || upstream.Code != "unsupported_request" {
				t.Fatalf("error = %v", err)
			}
		})
	}
	_, err := (ollama.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolResponses}, gateway.Target{})
	var upstream *gateway.UpstreamError
	if !errors.As(err, &upstream) || upstream.Code != "unsupported_protocol" {
		t.Fatalf("protocol error = %v", err)
	}
}

func decode(t *testing.T, body string, stream, usage bool) gateway.EventStream {
	t.Helper()
	request := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Stream: stream, ID: "test", Model: "client", Received: time.Unix(100, 0)}
	if usage {
		request.Body = []byte(`{"stream_options":{"include_usage":true}}`)
	}
	s := (ollama.Provider{}).Decode(request, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestNDJSONConvertsReasoningToolsUsageAndDone(t *testing.T) {
	body := `{"message":{"role":"assistant","content":""},"done":false}` + "\n" +
		`{"message":{"role":"assistant","content":"hi","thinking":"think"},"done":false}` + "\n" +
		`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"lookup","arguments":{"city":"Tokyo"}}}]},"done":false}` + "\n" +
		`{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":10,"eval_count":3}` + "\n"
	for _, wantUsage := range []bool{false, true} {
		s := decode(t, body, true, wantUsage)
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventData || ev.TextBytes != 7 || gjson.GetBytes(ev.Payload, "choices.0.delta.content").String() != "hi" || gjson.GetBytes(ev.Payload, "choices.0.delta.role").String() != "assistant" {
			t.Fatalf("first output = %+v %v", ev, err)
		}
		if gjson.GetBytes(ev.Payload, "choices.0.delta.reasoning_content").String() != "think" || gjson.GetBytes(ev.Payload, "model").String() != "client" {
			t.Fatalf("chunk = %s", ev.Payload)
		}
		ev, err = s.Next()
		if err != nil || ev.TextBytes != len(`{"city":"Tokyo"}`) || gjson.GetBytes(ev.Payload, "choices.0.delta.tool_calls.0.function.arguments").String() != `{"city":"Tokyo"}` || gjson.GetBytes(ev.Payload, "choices.0.delta.tool_calls.0.id").String() == "" {
			t.Fatalf("tool = %+v %v", ev, err)
		}
		ev, err = s.Next()
		if err != nil || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").String() != "tool_calls" {
			t.Fatalf("finish = %+v %v", ev, err)
		}
		ev, err = s.Next()
		if err != nil || ev.Usage == nil || ev.Usage.PromptTokens != 10 || ev.Usage.CompletionTokens != 3 || ev.Usage.Estimated {
			t.Fatalf("usage = %+v %v", ev, err)
		}
		if wantUsage {
			if ev.Kind != gateway.EventData || gjson.GetBytes(ev.Payload, "choices.#").Int() != 0 || gjson.GetBytes(ev.Payload, "usage.total_tokens").Int() != 13 {
				t.Fatalf("client usage = %+v", ev)
			}
		} else if ev.Kind != gateway.EventUsage || len(ev.Payload) != 0 {
			t.Fatalf("private usage = %+v", ev)
		}
		if ev, err = s.Next(); err != nil || ev.Kind != gateway.EventDone {
			t.Fatalf("done = %+v %v", ev, err)
		}
		if _, err := s.Next(); !errors.Is(err, io.EOF) {
			t.Fatalf("end = %v", err)
		}
	}
}

func TestNativeStreamFailureAndCut(t *testing.T) {
	for _, tc := range []struct {
		body, code string
	}{
		{`{"error":"model unavailable"}`, "ollama_error"},
		{`{"message":null,"done":true}`, "invalid_response"},
		{`{"message":{"role":"assistant"},"done":true}`, "empty_response"},
		{`{"message":{"role":"assistant","content":"hi"},"done":true,"eval_count":-1}`, "invalid_response"},
		{`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"lookup","arguments":"bad"}}]},"done":true}`, "invalid_response"},
		{`<html>upstream down</html>`, "invalid_response"},
	} {
		s := decode(t, tc.body, true, false)
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventError || ev.Err == nil || ev.Err.Code != tc.code {
			t.Fatalf("body %s = %+v %v", tc.body, ev, err)
		}
	}
	s := decode(t, `{"message":{"role":"assistant","content":"hi"},"done":false}`, true, false)
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Next(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("missing done = %v", err)
	}
	s = decode(t, `{"message":{"role":"assistant","content":""},"done":false}`+"\n"+`{"error":"failed"}`, true, false)
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError {
		t.Fatalf("lifecycle prefix escaped failover gate: %+v %v", ev, err)
	}
}

func TestNonStreamingChatAndZeroUsage(t *testing.T) {
	s := decode(t, `{"message":{"role":"assistant","content":"hi","thinking":"hmm"},"done":true,"done_reason":"length","prompt_eval_count":0,"eval_count":0}`, false, false)
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventData || ev.TextBytes != 5 || ev.Usage == nil || ev.Usage.PromptTokens != 0 || ev.Usage.CompletionTokens != 0 {
		t.Fatalf("response = %+v %v", ev, err)
	}
	if gjson.GetBytes(ev.Payload, "choices.0.message.content").String() != "hi" || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").String() != "length" || gjson.GetBytes(ev.Payload, "object").String() != "chat.completion" {
		t.Fatalf("response = %s", ev.Payload)
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("end = %v", err)
	}
	s = decode(t, `{"message":{"role":"assistant","content":"hi"},"done":false}`, false, false)
	if ev, err := s.Next(); err != nil || ev.Kind != gateway.EventError {
		t.Fatalf("incomplete single response = %+v %v", ev, err)
	}
}
