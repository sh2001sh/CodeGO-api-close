package xunfei

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func nativeFrame(status, seq int, text, usage string) string {
	if usage == "" {
		usage = "null"
	}
	return fmt.Sprintf(`{"header":{"code":0,"sid":"spark-fixture"},"payload":{"choices":{"status":%d,"seq":%d,"text":[{"content":%q,"role":"assistant","index":0}]},"usage":{"text":%s}}}`, status, seq, text, usage)
}

func replay(stream bool, body string, frames ...string) gateway.EventStream {
	data := ""
	for _, frame := range frames {
		data += "data: " + frame + "\n\n"
	}
	return (Provider{}).Decode(chatRequest(stream, body), &http.Response{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(data))})
}

func TestStreamTextFinishAndActualCumulativeUsage(t *testing.T) {
	s := replay(true, `{"stream_options":{"include_usage":true}}`, nativeFrame(0, 0, "你好", `{"prompt_tokens":5,"completion_tokens":1}`), nativeFrame(2, 1, "!", `{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}`))
	defer func() { _ = s.Close() }()
	first, err := s.Next()
	if err != nil || first.Kind != gateway.EventData || first.TextBytes != 6 || gjson.GetBytes(first.Payload, "choices.0.delta.role").Str != "assistant" {
		t.Fatalf("first chunk: %+v %v", first, err)
	}
	last, err := s.Next()
	if err != nil || last.TextBytes != 1 || gjson.GetBytes(last.Payload, "choices.0.finish_reason").Str != "stop" {
		t.Fatalf("terminal chunk: %+v %v", last, err)
	}
	usage, err := s.Next()
	if err != nil || usage.Usage.PromptTokens != 5 || usage.Usage.CompletionTokens != 3 || len(gjson.GetBytes(usage.Payload, "choices").Array()) != 0 || gjson.GetBytes(usage.Payload, "usage.total_tokens").Int() != 8 {
		t.Fatalf("usage must be cumulative, not added: %+v %v", usage, err)
	}
	done, err := s.Next()
	if err != nil || done.Kind != gateway.EventDone {
		t.Fatalf("missing done: %+v %v", done, err)
	}
	if _, err = s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("after done: %v", err)
	}
}

func TestCollectedChatWaitsForTerminalAndCombinesText(t *testing.T) {
	s := replay(false, `{}`, nativeFrame(0, 0, "hello ", ""), nativeFrame(2, 1, "world", `{"prompt_tokens":10,"completion_tokens":2}`))
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventData || ev.TextBytes != 11 || ev.Usage.CompletionTokens != 2 || gjson.GetBytes(ev.Payload, "choices.0.message.content").Str != "hello world" || gjson.GetBytes(ev.Payload, "model").Str != "SparkDesk-v3.5" {
		t.Fatalf("collected: %+v %v", ev, err)
	}
	if _, err = s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("collected completion end: %v", err)
	}
}

func TestEmptyTerminalDoesNotCommitLifecycleData(t *testing.T) {
	for _, stream := range []bool{true, false} {
		s := replay(stream, `{}`, nativeFrame(0, 0, "", ""), nativeFrame(2, 1, "", `{"prompt_tokens":3,"completion_tokens":0}`))
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventUsage || ev.Usage.PromptTokens != 3 {
			t.Fatalf("empty must only expose usage: %+v %v", ev, err)
		}
		ev, err = s.Next()
		if err != nil || ev.Kind != gateway.EventDone {
			t.Fatalf("empty end: %+v %v", ev, err)
		}
		_ = s.Close()
	}
}

func TestTruncatedOrMalformedNeverFinishesCleanly(t *testing.T) {
	for name, frames := range map[string][]string{
		"empty": {}, "truncated": {nativeFrame(0, 0, "partial", "")}, "bad_json": {"{"}, "bad_header": {"{}"},
		"bad_status": {nativeFrame(4, 0, "hi", "")}, "negative_usage": {nativeFrame(2, 0, "hi", `{"prompt_tokens":-1}`)},
		"duplicate_sequence": {nativeFrame(0, 1, "a", ""), nativeFrame(2, 1, "b", "")},
		"overflow_usage":     {nativeFrame(2, 0, "hi", `{"prompt_tokens":9223372036854775807,"completion_tokens":1}`)},
	} {
		t.Run(name, func(t *testing.T) {
			s := replay(true, `{}`, frames...)
			defer func() { _ = s.Close() }()
			for i := 0; i < 5; i++ {
				ev, err := s.Next()
				if err != nil {
					if errors.Is(err, io.EOF) {
						t.Fatal("invalid response ended cleanly")
					}
					return
				}
				if ev.Kind == gateway.EventDone {
					t.Fatal("invalid response emitted Done")
				}
			}
			t.Fatal("invalid stream did not fail")
		})
	}
}

func TestUpstreamErrorDoesNotExposeReflectedCredentials(t *testing.T) {
	s := replay(true, `{}`, `{"header":{"code":10013,"message":"secret-key in authorization=reflected"}}`)
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError || ev.Err.Status != http.StatusTooManyRequests || ev.Err.Code != "spark_10013" || strings.Contains(ev.Err.Error(), "secret-key") {
		t.Fatalf("unsafe error: %+v %v", ev, err)
	}
}

func TestPartialResponseThenUpstreamErrorRetainsPartialOutput(t *testing.T) {
	s := replay(true, `{}`, nativeFrame(0, 0, "partial", `{"prompt_tokens":4,"completion_tokens":1}`), `{"header":{"code":10007,"message":"upstream failed"}}`)
	defer func() { _ = s.Close() }()
	first, err := s.Next()
	if err != nil || first.Kind != gateway.EventData || first.TextBytes != 7 || first.Usage.CompletionTokens != 1 {
		t.Fatalf("lost partial response: %+v %v", first, err)
	}
	failure, err := s.Next()
	if err != nil || failure.Kind != gateway.EventError || failure.Err.Code != "spark_10007" {
		t.Fatalf("lost late upstream failure: %+v %v", failure, err)
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("error response did not end: %v", err)
	}
}

func TestUsageHiddenUnlessClientRequestsIt(t *testing.T) {
	s := replay(true, `{}`, nativeFrame(2, 0, "answer", `{"prompt_tokens":5,"completion_tokens":3}`))
	defer func() { _ = s.Close() }()
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventUsage || len(ev.Payload) != 0 || ev.Usage.CompletionTokens != 3 {
		t.Fatalf("unexpected forwarded usage: %+v %v", ev, err)
	}
}
