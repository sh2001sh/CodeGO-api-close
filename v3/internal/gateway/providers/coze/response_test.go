package coze_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/coze"
	"github.com/tidwall/gjson"
)

func TestNativeSSEConversionUsageAndNonStreamingCompletion(t *testing.T) {
	body := ": heartbeat\n\n" + event("conversation.chat.created", `{"status":"created"}`) +
		event("conversation.message.delta", `{"type":"function_call","content":"internal bot tool"}`) + answer("hello") +
		event("conversation.message.delta", `{"id":"m1","type":"answer","content":"!","reasoning_content":"think"}`) +
		event("conversation.message.completed", `{"id":"m1","role":"assistant","type":"answer","content_type":"text","content":"hello!","reasoning_content":"think"}`) +
		completed(`,"usage":{"input_count":10,"output_count":3,"token_count":13}`) + event("done", `[DONE]`)
	for _, stream := range []bool{true, false} {
		s := decode(t, body, stream)
		events := readAll(t, s)
		if stream {
			if len(events) != 5 || events[0].Kind != gateway.EventData || events[1].Kind != gateway.EventData || events[2].Kind != gateway.EventData || events[3].Usage == nil || events[4].Kind != gateway.EventDone {
				t.Fatalf("events = %+v", events)
			}
			if gjson.GetBytes(events[0].Payload, "choices.0.delta.content").Str != "hello" || gjson.GetBytes(events[0].Payload, "choices.0.delta.role").Str != "assistant" || events[0].TextBytes != 5 {
				t.Fatalf("first = %+v", events[0])
			}
			if gjson.GetBytes(events[1].Payload, "choices.0.delta.content").Str != "!" || gjson.GetBytes(events[1].Payload, "choices.0.delta.reasoning_content").Str != "think" || events[1].TextBytes != 6 {
				t.Fatalf("second = %+v", events[1])
			}
			if gjson.GetBytes(events[2].Payload, "choices.0.finish_reason").Str != "stop" {
				t.Fatalf("finish = %s", events[2].Payload)
			}
			if events[3].Kind != gateway.EventData || gjson.GetBytes(events[3].Payload, "choices.#").Int() != 0 || gjson.GetBytes(events[3].Payload, "usage.total_tokens").Int() != 13 {
				t.Fatalf("usage = %+v", events[3])
			}
			if events[3].Usage.PromptTokens != 10 || events[3].Usage.CompletionTokens != 3 || events[3].Usage.Estimated {
				t.Fatalf("accounting = %+v", events[3].Usage)
			}
		} else {
			if len(events) != 4 || events[0].Kind != gateway.EventUsage || events[1].Kind != gateway.EventUsage || events[0].TextBytes+events[1].TextBytes != 11 || len(events[0].Payload) != 0 || len(events[1].Payload) != 0 || events[2].Kind != gateway.EventData || events[2].TextBytes != 0 || events[3].Kind != gateway.EventDone || events[2].Usage == nil {
				t.Fatalf("events = %+v", events)
			}
			for path, want := range map[string]string{"object": "chat.completion", "model": "client", "id": "chatcmpl-test", "created": "100", "choices.0.message.content": "hello!", "choices.0.message.reasoning_content": "think", "usage.prompt_tokens": "10", "usage.completion_tokens": "3", "choices.0.finish_reason": "stop"} {
				if got := gjson.GetBytes(events[2].Payload, path).String(); got != want {
					t.Fatalf("%s = %q; %s", path, got, events[2].Payload)
				}
			}
		}
	}
}

func TestFinalOnlyAnswerAndPrivateOrMissingUsage(t *testing.T) {
	final := event("conversation.message.completed", `{"id":"m","type":"answer","role":"assistant","content":"final"}`)
	for _, usage := range []string{"", `,"usage":{}`, `,"usage":{"input_count":0,"output_count":0,"token_count":0}`} {
		for _, stream := range []bool{true, false} {
			r := chatRequest(stream)
			r.Body = []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
			s := (coze.Provider{}).Decode(r, &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(final + completed(usage)))})
			events := readAll(t, s)
			_ = s.Close()
			if events[0].TextBytes != 5 {
				t.Fatalf("final-only events = %+v", events)
			}
			var actual *gateway.Usage
			for _, ev := range events {
				if ev.Usage != nil {
					actual = ev.Usage
					if stream && (ev.Kind != gateway.EventUsage || len(ev.Payload) != 0) {
						t.Fatalf("usage escaped = %+v", ev)
					}
				}
			}
			if strings.Contains(usage, "input_count") {
				if actual == nil || actual.PromptTokens != 0 || actual.CompletionTokens != 0 || actual.Estimated {
					t.Fatalf("zero usage = %+v", actual)
				}
			} else if actual != nil {
				t.Fatalf("missing native counts fabricated = %+v", actual)
			}
		}
	}
}

func TestCompletedMessagePreservesSuffixWithoutDuplicatingDeltas(t *testing.T) {
	body := answer("prefix ") + event("conversation.message.completed", `{"id":"m1","type":"answer","content":"prefix final suffix"}`) + completed("")
	for _, stream := range []bool{true, false} {
		events := readAll(t, decode(t, body, stream))
		text := ""
		bytes := 0
		for _, ev := range events {
			bytes += ev.TextBytes
			if stream {
				text += gjson.GetBytes(ev.Payload, "choices.0.delta.content").Str
			} else if ev.Kind == gateway.EventData {
				text = gjson.GetBytes(ev.Payload, "choices.0.message.content").Str
			}
		}
		if text != "prefix final suffix" || bytes != len(text) {
			t.Fatalf("stream %v produced %q, bytes %d", stream, text, bytes)
		}
	}
}

func TestNativeErrorsMalformedResponsesAndEmptyOutput(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{event("conversation.chat.created", `{"status":"created"}`) + event("error", `{"code":4015,"message":"not authorized"}`), "4015"},
		{event("conversation.chat.failed", `{"status":"failed","last_error":{"code":5001,"message":"bot failed"}}`), "5001"},
		{event("conversation.chat.canceled", `{"status":"canceled"}`), "canceled"},
		{event("conversation.chat.requires_action", `{"status":"requires_action"}`), "requires_action"},
		{event("conversation.message.delta", `{"type":"answer","content":[]}`), "invalid_response"},
		{event("conversation.message.delta", `{"type":"answer","content":"x","role":"user"}`), "invalid_response"},
		{event("conversation.message.delta", `{"type":"answer","content":"x","content_type":"object_string"}`), "invalid_response"},
		{event("conversation.message.delta", `not JSON`), "invalid_response"},
		{event("unknown", `{}`), "invalid_response"},
		{completed(""), "empty_response"},
		{answer("x") + completed(`,"usage":{"input_count":1}`), "invalid_response"},
		{answer("x") + completed(`,"usage":{"input_count":-1,"output_count":1}`), "invalid_response"},
		{answer("x") + completed(`,"usage":{"input_count":1.5,"output_count":1}`), "invalid_response"},
		{answer("x") + completed(`,"usage":{"input_count":1,"output_count":1,"token_count":3}`), "invalid_response"},
		{answer("x") + completed(`,"last_error":{"code":5002,"message":"late native error"}`), "5002"},
		{answer("x") + event("conversation.message.completed", `{"id":"m1","type":"answer","content":"different"}`), "invalid_response"},
	} {
		for _, stream := range []bool{false, true} {
			s := decode(t, tc.body, stream)
			var last gateway.Event
			for _, ev := range readAll(t, s) {
				last = ev
			}
			if last.Kind != gateway.EventError || last.Err == nil || last.Err.Code != tc.code {
				t.Fatalf("body %s stream %v = %+v", tc.body, stream, last)
			}
		}
	}
	for _, body := range []string{`{"code":4015,"msg":"token expired"}`, `{"code":0,"data":{"status":"in_progress"}}`} {
		s := (coze.Provider{}).Decode(chatRequest(false), &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))})
		ev, err := s.Next()
		_ = s.Close()
		if err != nil || ev.Kind != gateway.EventError || ev.Err == nil {
			t.Fatalf("JSON response = %+v %v", ev, err)
		}
		if strings.Contains(body, "4015") && (ev.Err.Code != "4015" || ev.Err.Message != "token expired") {
			t.Fatalf("native error = %+v", ev.Err)
		}
	}
}

func TestNativeFailureRetainsRealUsageAndMessage(t *testing.T) {
	body := event("conversation.chat.failed", `{"status":"failed","last_error":{"code":5001,"message":"bot failed"},"usage":{"input_count":4,"output_count":2,"token_count":6}}`)
	for _, stream := range []bool{true, false} {
		s := decode(t, body, stream)
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventError || ev.Err.Code != "5001" || ev.Err.Message != "bot failed" || ev.Usage == nil || ev.Usage.PromptTokens != 4 || ev.Usage.CompletionTokens != 2 || ev.Usage.Estimated {
			t.Fatalf("native failure = %+v %v", ev, err)
		}
	}
}

func TestStreamCutNeverBecomesCleanCompletion(t *testing.T) {
	for _, body := range []string{"", event("conversation.chat.created", `{"status":"created"}`), answer("partial"), answer("partial") + "event: conversation.chat.completed\ndata: {\"status\":\"completed\"}", event("done", "[DONE]")} {
		for _, stream := range []bool{true, false} {
			s := decode(t, body, stream)
			for {
				ev, err := s.Next()
				if err != nil {
					if !errors.Is(err, io.ErrUnexpectedEOF) {
						t.Fatalf("cut error = %v", err)
					}
					break
				}
				if ev.Kind == gateway.EventDone {
					t.Fatal("cut reported clean completion")
				}
				if !stream && ev.Kind == gateway.EventData {
					t.Fatal("partial answer escaped blocking response")
				}
			}
		}
	}
}

func TestMockServerUsesNativeStreamingForBlockingClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v3/chat" || r.Header.Get("Authorization") != "Bearer token" || !gjson.GetBytes(body, "stream").Bool() || gjson.GetBytes(body, "bot_id").Str != "bot" {
			t.Errorf("native request = %s %s", r.URL, body)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, answer("mock answer")+completed(`,"usage":{"input_count":4,"output_count":2,"token_count":6}`))
	}))
	defer server.Close()
	r := chatRequest(false)
	out, err := (coze.Provider{}).BuildRequest(context.Background(), r, gateway.Target{BaseURL: server.URL, Secret: "bot|token"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(out)
	if err != nil {
		t.Fatal(err)
	}
	s := (coze.Provider{}).Decode(r, resp)
	defer func() { _ = s.Close() }()
	var completion gateway.Event
	for _, ev := range readAll(t, s) {
		if ev.Kind == gateway.EventData {
			completion = ev
		}
	}
	if completion.Usage == nil || completion.Usage.PromptTokens != 4 || gjson.GetBytes(completion.Payload, "choices.0.message.content").Str != "mock answer" {
		t.Fatalf("mock response = %+v", completion)
	}
}

func TestCancellationAndDeadlineInterruptNativeStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, event("conversation.chat.created", `{"status":"created"}`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if mode == "cancel" {
				ctx, cancel = context.WithCancel(context.Background())
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 150*time.Millisecond)
			}
			defer cancel()
			r := chatRequest(false)
			out, err := (coze.Provider{}).BuildRequest(ctx, r, gateway.Target{BaseURL: server.URL, Secret: "bot|token"})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := server.Client().Do(out)
			if err != nil {
				t.Fatal(err)
			}
			s := (coze.Provider{}).Decode(r, resp)
			defer func() { _ = s.Close() }()
			result := make(chan error, 1)
			go func() { _, err := s.Next(); result <- err }()
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-result:
				want := context.Canceled
				if mode == "deadline" {
					want = context.DeadlineExceeded
				}
				if !errors.Is(err, want) {
					t.Fatalf("%s = %v, want %v", mode, err, want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("canceled stream remained blocked")
			}
		})
	}
}
