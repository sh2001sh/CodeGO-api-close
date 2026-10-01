package dify

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
	"github.com/tidwall/gjson"
)

func decode(body string, streaming, usage bool) gateway.EventStream {
	req := chatRequest(`{"stream_options":{"include_usage":`+map[bool]string{true: "true", false: "false"}[usage]+`}}`, streaming)
	return (Provider{}).Decode(req, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))})
}

func TestBlockingSuccessAndMissingUsage(t *testing.T) {
	for _, usage := range []string{`,"metadata":{"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`, ``, `,"metadata":{"usage":{"prompt_tokens":7}}`} {
		stream := decode(`{"event":"message","id":"native-message","conversation_id":"conv","answer":"answer"`+usage+`}`, false, false)
		ev, err := stream.Next()
		if err != nil || ev.Kind != gateway.EventData || ev.TextBytes != 6 {
			t.Fatalf("bad completion: %+v %v", ev, err)
		}
		if gjson.GetBytes(ev.Payload, "choices.0.message.content").Str != "answer" || gjson.GetBytes(ev.Payload, "model").Str != "app-alias" || gjson.GetBytes(ev.Payload, "id").Str != "native-message" || gjson.GetBytes(ev.Payload, "conversation_id").Str != "conv" {
			t.Fatalf("bad native conversion: %s", ev.Payload)
		}
		if strings.Contains(usage, "completion_tokens") {
			if ev.Usage == nil || ev.Usage.PromptTokens != 7 || ev.Usage.CompletionTokens != 3 {
				t.Fatalf("lost usage: %+v", ev.Usage)
			}
		} else if ev.Usage != nil || gjson.GetBytes(ev.Payload, "usage").Exists() {
			t.Fatal("fabricated missing usage")
		}
		if _, err := stream.Next(); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		_ = stream.Close()
	}
}

func TestCacheAndImageUsageSurvivesConversion(t *testing.T) {
	stream := decode(`{"answer":"image description","metadata":{"usage":{"prompt_tokens":100,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":40,"cache_write_tokens":10,"image_tokens":20},"claude_cache_creation_1_h_tokens":2}}}`, false, false)
	defer func() { _ = stream.Close() }()
	ev, err := stream.Next()
	if err != nil || ev.Usage == nil || ev.Usage.CachedTokens != 40 || ev.Usage.CacheWriteTokens != 10 || ev.Usage.ImageInputTokens != 20 || ev.Usage.CacheWrite1hTokens != 2 || gjson.GetBytes(ev.Payload, "usage.prompt_tokens_details.cached_tokens").Int() != 40 {
		t.Fatalf("billing usage lost: %+v %s %v", ev.Usage, ev.Payload, err)
	}
}

func TestStreamConversationIsExposedForResuming(t *testing.T) {
	stream := decode("data: {\"event\":\"message\",\"answer\":\"hello\",\"conversation_id\":\"resume-id\"}\n\ndata: {\"event\":\"message_end\"}\n\n", true, false)
	defer func() { _ = stream.Close() }()
	ev, err := stream.Next()
	if err != nil || gjson.GetBytes(ev.Payload, "conversation_id").Str != "resume-id" {
		t.Fatalf("conversation cannot be resumed: %s %v", ev.Payload, err)
	}
}

func TestErrorsCannotBecomeSuccessfulCompletion(t *testing.T) {
	for name, body := range map[string]string{
		"native error":      `{"code":"app_unavailable","message":"App disabled","status":400}`,
		"error event":       `{"event":"error","code":"model_error","message":"Model failed"}`,
		"no answer":         `{"conversation_id":"conv"}`,
		"wrong answer type": `{"answer":42}`,
		"empty answer":      `{"answer":""}`,
		"not a response":    `[]`,
		"bad usage":         `{"answer":"x","metadata":{"usage":{"prompt_tokens":-1,"completion_tokens":2}}}`,
		"fractional usage":  `{"answer":"x","metadata":{"usage":{"prompt_tokens":1.5,"completion_tokens":2}}}`,
		"mismatched totals": `{"answer":"x","metadata":{"usage":{"prompt_tokens":2,"completion_tokens":2,"total_tokens":3}}}`,
		"overflow":          `{"answer":"x","metadata":{"usage":{"prompt_tokens":9223372036854775807,"completion_tokens":1}}}`,
		"incomplete event":  `{"event":"workflow_started","answer":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			stream := decode(body, false, false)
			defer func() { _ = stream.Close() }()
			ev, err := stream.Next()
			if err != nil || ev.Kind != gateway.EventError || ev.Err == nil {
				t.Fatalf("error incorrectly accepted: %+v %v", ev, err)
			}
			if name == "native error" && (ev.Err.Code != "app_unavailable" || ev.Err.Message != "App disabled") {
				t.Fatalf("lost error details: %+v", ev.Err)
			}
			if _, err := stream.Next(); !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
		})
	}
}

func TestStreamTerminalAndUsage(t *testing.T) {
	for _, usage := range []string{``, `,"metadata":{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`} {
		stream := decode("event: ping\n\ndata: {\"event\":\"workflow_started\"}\n\ndata: {\"event\":\"agent_thought\",\"thought\":\"internal\"}\n\nevent: agent_message\ndata: {\"answer\":\"hello\"}\n\ndata: {\"event\":\"workflow_finished\",\"data\":{\"status\":\"succeeded\"}}\n\ndata: {\"event\":\"message_end\""+usage+"}\n\n", true, false)
		defer func() { _ = stream.Close() }()
		ev, err := stream.Next()
		if err != nil || ev.Kind != gateway.EventData || gjson.GetBytes(ev.Payload, "choices.0.delta.content").Str != "hello" || gjson.GetBytes(ev.Payload, "choices.0.delta.role").Str != "assistant" {
			t.Fatalf("lifecycle committed before answer: %+v %v", ev, err)
		}
		ev, err = stream.Next()
		if err != nil || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str != "stop" {
			t.Fatalf("missing finish: %+v %v", ev, err)
		}
		if usage != "" {
			ev, err = stream.Next()
			if err != nil || ev.Kind != gateway.EventUsage || ev.Usage == nil || ev.Usage.Estimated || ev.Usage.CompletionTokens != 0 {
				t.Fatalf("reported zero usage lost: %+v %v", ev, err)
			}
		}
		ev, err = stream.Next()
		if err != nil || ev.Kind != gateway.EventDone {
			t.Fatalf("missing Done: %+v %v", ev, err)
		}
	}
}

func TestStreamCutAndWorkflowCompletionAreNotTerminals(t *testing.T) {
	for _, tail := range []string{``, "data: {\"event\":\"message_end\"}", "data: {\"event\":\"workflow_finished\",\"data\":{\"status\":\"succeeded\"}}\n\n", "data: [DONE]\n\n"} {
		stream := decode("data: {\"event\":\"message\",\"answer\":\"partial\"}\n\n"+tail, true, false)
		defer func() { _ = stream.Close() }()
		if ev, err := stream.Next(); err != nil || ev.Kind != gateway.EventData {
			t.Fatalf("missing partial: %+v %v", ev, err)
		}
		ev, err := stream.Next()
		if tail == "data: [DONE]\n\n" {
			if err != nil || ev.Kind != gateway.EventError {
				t.Fatalf("accepted OpenAI Done: %+v %v", ev, err)
			}
		} else if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("cut stream marked successful: %+v %v", ev, err)
		}
	}
}

func TestStreamErrorReplacementAndEmptyOutput(t *testing.T) {
	for _, body := range []string{
		"data: {\"event\":\"error\",\"code\":\"failed\",\"message\":\"Failed\"}\n\n",
		"data: {\"event\":\"message_replace\",\"answer\":\"replaced\"}\n\n",
		"data: {\"event\":\"message_end\"}\n\n",
		"data: {\"event\":\"workflow_finished\",\"data\":{\"status\":\"failed\"}}\n\n",
		"data: {\"event\":\"message\",\"answer\":42}\n\n",
	} {
		stream := decode(body, true, false)
		defer func() { _ = stream.Close() }()
		ev, err := stream.Next()
		if err != nil || ev.Kind != gateway.EventError {
			t.Fatalf("invalid stream accepted: %+v %v", ev, err)
		}
	}
}

func TestCancellationAndDeadlineRemainTransportFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"event\":\"workflow_started\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "canceled", true: "deadline"}[deadline], func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if deadline {
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			req := chatRequest(`{"messages":[{"role":"user","content":"x"}]}`, true)
			out, err := (Provider{}).BuildRequest(ctx, req, gateway.Target{BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := server.Client().Do(out)
			if err != nil {
				t.Fatal(err)
			}
			stream := (Provider{}).Decode(req, resp)
			defer func() { _ = stream.Close() }()
			if !deadline {
				cancel()
			}
			_, err = stream.Next()
			expected := context.Canceled
			if deadline {
				expected = context.DeadlineExceeded
			}
			if !errors.Is(err, expected) {
				t.Fatalf("transport failure became completion: %v, want %v", err, expected)
			}
		})
	}
}
