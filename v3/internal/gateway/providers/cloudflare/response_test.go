package cloudflare_test

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
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/cloudflare"
	"github.com/tidwall/gjson"
)

func decode(t *testing.T, body string, stream, usage bool) gateway.EventStream {
	t.Helper()
	request := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "public", ID: "test", Received: time.Unix(100, 0), Stream: stream}
	if usage {
		request.Body = []byte(`{"stream_options":{"include_usage":true}}`)
	}
	result := (cloudflare.Provider{}).Decode(request, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	t.Cleanup(func() { _ = result.Close() })
	return result
}

func TestWrapperJSONUsesActualUsageAndClientModel(t *testing.T) {
	s := decode(t, `{"result":{"response":"hello","usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":3}}},"success":true,"errors":[],"messages":[]}`, false, false)
	event, err := s.Next()
	if err != nil || event.Kind != gateway.EventData || event.TextBytes != 5 || event.Usage == nil ||
		event.Usage.PromptTokens != 10 || event.Usage.CompletionTokens != 2 || event.Usage.CachedTokens != 3 || event.Usage.Estimated {
		t.Fatalf("native usage = %+v %v", event, err)
	}
	for path, want := range map[string]string{"id": "chatcmpl-test", "object": "chat.completion", "model": "public", "created": "100",
		"choices.0.message.role": "assistant", "choices.0.message.content": "hello", "choices.0.finish_reason": "stop", "usage.total_tokens": "12"} {
		if got := gjson.GetBytes(event.Payload, path).String(); got != want {
			t.Fatalf("%s = %q, want %q; response = %s", path, got, want, event.Payload)
		}
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("non-stream EOF = %v", err)
	}
}

func TestMissingPartialAndExplicitZeroUsageAreDistinct(t *testing.T) {
	for _, body := range []string{
		`{"result":{"response":"hello"},"success":true}`,
		`{"result":{"response":"hello","usage":{"prompt_tokens":2}},"success":true}`,
		`{"result":{"response":"hello"},"usage":{"prompt_tokens":2},"success":true}`,
	} {
		event, err := decode(t, body, false, false).Next()
		if err != nil || event.Kind != gateway.EventData || event.Usage != nil || gjson.GetBytes(event.Payload, "usage").Exists() {
			t.Fatalf("missing usage fabricated: %+v %v", event, err)
		}
	}
	for _, body := range []string{
		`{"result":{"response":"hello","usage":{"prompt_tokens":0,"completion_tokens":0}},"success":true}`,
		`{"result":{"response":"hello"},"usage":{"prompt_tokens":0,"completion_tokens":0},"success":true}`,
	} {
		event, err := decode(t, body, false, false).Next()
		if err != nil || event.Usage == nil || event.Usage.PromptTokens != 0 || event.Usage.CompletionTokens != 0 || event.Usage.Estimated {
			t.Fatalf("explicit zero usage lost: %+v %v", event, err)
		}
	}
}

func TestNativeSSEConvertsTextUsageAndRequiresDone(t *testing.T) {
	body := ": keepalive\n\ndata: {\"response\":\"\"}\n\n" +
		"data: {\"response\":\"hello \"}\r\n\r\n" +
		"data: {\"result\":{\"response\":\"world\"},\"success\":true}\n\n" +
		"data: {\"response\":\"\",\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}\n\n" +
		"data: [DONE]\n\n"
	for _, includeUsage := range []bool{false, true} {
		s := decode(t, body, true, includeUsage)
		for index, want := range []string{"hello ", "world"} {
			event, err := s.Next()
			if err != nil || event.Kind != gateway.EventData || event.TextBytes != len(want) || gjson.GetBytes(event.Payload, "choices.0.delta.content").String() != want || gjson.GetBytes(event.Payload, "model").String() != "public" {
				t.Fatalf("native chunk = %+v %v", event, err)
			}
			if role := gjson.GetBytes(event.Payload, "choices.0.delta.role").String(); index == 0 && role != "assistant" || index > 0 && role != "" {
				t.Fatalf("chunk role = %q", role)
			}
		}
		event, err := s.Next()
		if err != nil || event.Kind != gateway.EventUsage || event.Usage == nil || event.Usage.PromptTokens != 4 || event.Usage.CompletionTokens != 2 || event.Usage.Estimated {
			t.Fatalf("native usage = %+v %v", event, err)
		}
		event, err = s.Next()
		if err != nil || event.Kind != gateway.EventData || gjson.GetBytes(event.Payload, "choices.0.finish_reason").String() != "stop" {
			t.Fatalf("finish = %+v %v", event, err)
		}
		if includeUsage {
			event, err = s.Next()
			if err != nil || event.Kind != gateway.EventData || event.Usage == nil || gjson.GetBytes(event.Payload, "choices.#").Int() != 0 || gjson.GetBytes(event.Payload, "usage.total_tokens").Int() != 6 {
				t.Fatalf("client usage = %+v %v", event, err)
			}
		}
		event, err = s.Next()
		if err != nil || event.Kind != gateway.EventDone {
			t.Fatalf("done = %+v %v", event, err)
		}
		if _, err := s.Next(); !errors.Is(err, io.EOF) {
			t.Fatalf("stream EOF = %v", err)
		}
	}
}

func TestNoUsageStreamDoesNotFabricateTokenCounts(t *testing.T) {
	s := decode(t, "data: {\"response\":\"hi\"}\n\ndata: [DONE]\n\n", true, true)
	for _, want := range []gateway.EventKind{gateway.EventData, gateway.EventData, gateway.EventDone} {
		event, err := s.Next()
		if err != nil || event.Kind != want || event.Usage != nil || gjson.GetBytes(event.Payload, "usage").Exists() {
			t.Fatalf("no-usage stream = %+v %v", event, err)
		}
	}
}

func TestNativeErrorsInvalidPayloadAndInvalidUsage(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{`{"success":false,"errors":[{"code":10000,"message":"authentication error"}]}`, "cloudflare_10000"},
		{`{"success":false,"result":{"response":"must not forward"}}`, "cloudflare_error"},
		{`{"result":{"error":{"code":"model_unavailable","message":"down"}},"success":true}`, "cloudflare_model_unavailable"},
		{`{"error":"failed"}`, "cloudflare_error"},
		{`{"result":{"response":""},"success":true}`, "empty_response"},
		{`{"result":null,"success":true}`, "invalid_response"},
		{`{"result":"text","success":true}`, "invalid_response"},
		{`{"result":{},"success":true}`, "invalid_response"},
		{`{"result":{"response":42},"success":true}`, "invalid_response"},
		{`{"response":"text","tool_calls":[{"name":"unexpected"}]}`, "invalid_response"},
		{`{"response":"text","usage":{"prompt_tokens":-1,"completion_tokens":2}}`, "invalid_response"},
		{`{"response":"text","usage":{"prompt_tokens":1,"completion_tokens":"2"}}`, "invalid_response"},
		{`{"response":"text","usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":4}}`, "invalid_response"},
		{`{"response":"text","usage":{"prompt_tokens":9223372036854775807,"completion_tokens":1}}`, "invalid_response"},
		{`{"response":"text","usage":{"prompt_tokens":1,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":2}}}`, "invalid_response"},
		{`<html>down</html>`, "invalid_response"},
		{`[]`, "invalid_response"},
	} {
		for _, stream := range []bool{false, true} {
			body := tc.body
			if stream {
				body = "data: " + body + "\n\ndata: [DONE]\n\n"
			}
			event, err := decode(t, body, stream, false).Next()
			if err != nil || event.Kind != gateway.EventError || event.Err == nil || event.Err.Code != tc.code {
				t.Fatalf("body %s stream %v = %+v %v", tc.body, stream, event, err)
			}
		}
	}
	s := decode(t, "data: {\"response\":\"\"}\n\ndata: {\"error\":\"failure\"}\n\n", true, false)
	event, err := s.Next()
	if err != nil || event.Kind != gateway.EventError || event.Err.Code != "cloudflare_error" {
		t.Fatalf("empty prefix committed attempt: %+v %v", event, err)
	}
	event, err = decode(t, "event: error\ndata: {\"code\":10000,\"message\":\"stream failed\"}\n\n", true, false).Next()
	if err != nil || event.Kind != gateway.EventError || event.Err.Code != "cloudflare_10000" || event.Err.Message != "stream failed" {
		t.Fatalf("named native error = %+v %v", event, err)
	}
}

func TestStreamErrorPreservesOnlyValidatedActualUsage(t *testing.T) {
	for _, tc := range []struct {
		name, failure, code string
		wantUsage           bool
		prompt, completion  int64
	}{
		{"direct", `data: {"error":"failed","usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`, "cloudflare_error", true, 4, 2},
		{"wrapped_outer_usage", `data: {"success":true,"result":{"error":{"code":10000,"message":"failed"}},"usage":{"prompt_tokens":4,"completion_tokens":2}}`, "cloudflare_10000", true, 4, 2},
		{"wrapped_inner_usage", `data: {"success":true,"result":{"error":"failed","usage":{"prompt_tokens":4,"completion_tokens":2}}}`, "cloudflare_error", true, 4, 2},
		{"wrapper_error", `data: {"success":false,"errors":[{"code":10000,"message":"failed"}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`, "cloudflare_10000", true, 4, 2},
		{"named_error", "event: error\ndata: " + `{"code":10000,"message":"failed","usage":{"prompt_tokens":4,"completion_tokens":2}}`, "cloudflare_10000", true, 4, 2},
		{"explicit_zero", `data: {"error":"failed","usage":{"prompt_tokens":0,"completion_tokens":0}}`, "cloudflare_error", true, 0, 0},
		{"missing", `data: {"error":"failed"}`, "cloudflare_error", false, 0, 0},
		{"partial", `data: {"error":"failed","usage":{"prompt_tokens":4}}`, "cloudflare_error", false, 0, 0},
		{"malformed", `data: {"error":"failed","usage":{"prompt_tokens":"4","completion_tokens":2}}`, "invalid_response", false, 0, 0},
		{"negative", `data: {"error":"failed","usage":{"prompt_tokens":-1,"completion_tokens":2}}`, "invalid_response", false, 0, 0},
		{"overflow", `data: {"error":"failed","usage":{"prompt_tokens":9223372036854775807,"completion_tokens":1}}`, "invalid_response", false, 0, 0},
		{"inconsistent", `data: {"error":"failed","usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":1}}`, "invalid_response", false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := decode(t, "data: {\"response\":\"partial\"}\n\n"+tc.failure+"\n\n", true, true)
			first, err := s.Next()
			if err != nil || first.Kind != gateway.EventData || gjson.GetBytes(first.Payload, "choices.0.delta.content").String() != "partial" {
				t.Fatalf("partial output = %+v %v", first, err)
			}
			event, err := s.Next()
			if err != nil || event.Kind != gateway.EventError || event.Err == nil || event.Err.Code != tc.code {
				t.Fatalf("native error = %+v %v", event, err)
			}
			if !tc.wantUsage {
				if event.Usage != nil {
					t.Fatalf("unvalidated usage became authoritative: %+v", event.Usage)
				}
			} else if event.Usage == nil || event.Usage.Estimated || event.Usage.PromptTokens != tc.prompt || event.Usage.CompletionTokens != tc.completion {
				t.Fatalf("native error discarded actual usage: %+v", event.Usage)
			}
			if _, err := s.Next(); !errors.Is(err, io.EOF) {
				t.Fatalf("error must terminate without fabricating done: %v", err)
			}
		})
	}
}

func TestTruncatedStreamsNeverReportCleanEnd(t *testing.T) {
	for _, body := range []string{
		"data: {\"response\":\"hi\"}\n\n",
		"data: {\"response\":\"hi\"}\n\ndata: {\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n",
		"data: {\"response\":\"hi\"}\n\ndata: [DONE]\n", // Truncated SSE event framing.
	} {
		s := decode(t, body, true, false)
		for {
			event, err := s.Next()
			if err != nil {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("truncation error = %v", err)
				}
				break
			}
			if event.Kind == gateway.EventDone || event.Kind == gateway.EventError {
				t.Fatalf("truncation misclassified: %+v", event)
			}
		}
	}
	event, err := decode(t, "data: [DONE]\n\n", true, false).Next()
	if err != nil || event.Kind != gateway.EventError || event.Err.Code != "empty_response" {
		t.Fatalf("empty DONE stream = %+v %v", event, err)
	}
}

func TestHTTPRoundTripHonorsCancellationAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/client/v4/accounts/account/ai/run/@cf/meta/model" || request.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("request URL/auth not preserved: %s %v", request.URL, request.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"response\":\"prefix\"}\n\n")
		w.(http.Flusher).Flush()
		<-request.Context().Done()
	}))
	defer server.Close()
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancellation", true: "deadline"}[timeout], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if timeout {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
			}
			defer cancel()
			in := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "@cf/meta/model", Stream: true,
				Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}
			request, err := (cloudflare.Provider{}).BuildRequest(ctx, in, gateway.Target{BaseURL: server.URL, Secret: "account|token"})
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			s := (cloudflare.Provider{}).Decode(in, response)
			defer func() { _ = s.Close() }()
			event, err := s.Next()
			if err != nil || event.Kind != gateway.EventData || gjson.GetBytes(event.Payload, "choices.0.delta.content").String() != "prefix" {
				t.Fatalf("prefix = %+v %v", event, err)
			}
			want := context.Canceled
			if timeout {
				want = context.DeadlineExceeded
			} else {
				cancel()
			}
			if _, err := s.Next(); !errors.Is(err, want) {
				t.Fatalf("body read cancellation = %v, want %v", err, want)
			}
		})
	}
}
