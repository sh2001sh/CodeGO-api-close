package zhipu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

func fixture(t *testing.T, wire string, stream, usage bool) gateway.EventStream {
	t.Helper()
	body := []byte(`{"stream_options":{"include_usage":false}}`)
	if usage {
		body = []byte(`{"stream_options":{"include_usage":true}}`)
	}
	s := (Provider{}).Decode(&gateway.Request{ID: "test", Model: "client", Received: time.Unix(100, 0), Stream: stream, Body: body}, &http.Response{Body: io.NopCloser(strings.NewReader(wire))})
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestNativeFinishMetaUsageAndQuotedTextPreserved(t *testing.T) {
	wire := ": keepalive\r\n\r\nevent: add\r\ndata: hello\r\ndata: world\r\n\r\nevent: finish\r\ndata: !\r\nmeta: {\"task_id\":\"task1\",\"task_status\":\"SUCCESS\",\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":3}}\r\n\r\n"
	for _, include := range []bool{false, true} {
		s := fixture(t, wire, true, include)
		var texts []string
		var exact, finish, done, clientUsage bool
		for i := 0; i < 10; i++ {
			ev, err := s.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil || ev.Kind == gateway.EventError {
				t.Fatalf("event = %+v %v", ev, err)
			}
			if ev.Usage != nil {
				if ev.Usage.PromptTokens != 9 || ev.Usage.CompletionTokens != 3 || ev.Usage.Estimated {
					t.Fatalf("usage = %+v", ev.Usage)
				}
				exact = true
			}
			if ev.TextBytes > 0 {
				text := gjson.GetBytes(ev.Payload, "choices.0.delta.content").Str
				if len(text) != ev.TextBytes {
					t.Fatalf("text bytes = %d; payload = %s", ev.TextBytes, ev.Payload)
				}
				texts = append(texts, text)
			}
			if gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str == "stop" {
				finish = true
				if gjson.GetBytes(ev.Payload, "id").Str != "task1" {
					t.Fatalf("finish id = %s", ev.Payload)
				}
			}
			if gjson.GetBytes(ev.Payload, "usage.total_tokens").Int() == 12 {
				clientUsage = true
				if gjson.GetBytes(ev.Payload, "choices.#").Int() != 0 {
					t.Fatalf("usage chunk = %s", ev.Payload)
				}
			}
			done = done || ev.Kind == gateway.EventDone
		}
		if strings.Join(texts, "") != "hello\nworld!" || !exact || !finish || !done || clientUsage != include {
			t.Fatalf("text=%v usage=%v finish=%v done=%v clientUsage=%v", texts, exact, finish, done, clientUsage)
		}
	}
}

func TestSinglesPreserveUsageAndDoNotStripMeaningfulQuotes(t *testing.T) {
	wire := `{"success":true,"code":200,"data":{"task_id":"task1","task_status":"SUCCESS","choices":[{"role":"assistant","content":"\"answer\""}],"usage":{"prompt_tokens":0,"completion_tokens":0}}}`
	s := fixture(t, wire, false, false)
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventData || ev.Usage == nil || ev.Usage.PromptTokens != 0 || ev.Usage.CompletionTokens != 0 || ev.TextBytes != 8 || gjson.GetBytes(ev.Payload, "choices.0.message.content").Str != `"answer"` || gjson.GetBytes(ev.Payload, "model").Str != "client" {
		t.Fatalf("single = %+v %v", ev, err)
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("single end = %v", err)
	}
	for _, wire := range []string{
		`{"success":false,"code":429,"msg":"busy"}`,
		`{"success":true,"data":{"task_status":"FAIL","choices":[{"content":"discard"}]}}`,
		`{"success":true,"data":{"choices":[]}}`,
		`{"success":true,"data":{"choices":[{"content":""}]}}`,
		`{"success":true,"data":{"choices":[{"content":"a"},{"content":"b"}]}}`,
		`{"data":{"choices":[{"content":"not confirmed"}]}}`,
	} {
		s = fixture(t, wire, false, false)
		if ev, err := s.Next(); err != nil || ev.Kind != gateway.EventError {
			t.Fatalf("wire=%s event=%+v error=%v", wire, ev, err)
		}
	}
}

func TestErrorsCutsNoUsageAndEmptyNeverBecomeFakeSuccess(t *testing.T) {
	for _, after := range []bool{false, true} {
		prefix := ""
		if after {
			prefix = "event: add\ndata: partial\n\n"
		}
		for _, ending := range []string{"", "event: finish\ndata: incomplete", "event: add\ndata: cut\n"} {
			s := fixture(t, prefix+ending, true, false)
			if after {
				if ev, err := s.Next(); err != nil || ev.Kind != gateway.EventData || ev.TextBytes != 7 {
					t.Fatalf("partial = %+v %v", ev, err)
				}
			}
			if _, err := s.Next(); !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("cut = %v", err)
			}
		}
		for _, ending := range []string{"event: error\ndata: {\"code\":500,\"msg\":\"busy\"}\n\n", "event: interrupted\ndata: stopped\n\n", "event: finish\nmeta: {\"task_status\":\"FAIL\"}\n\n"} {
			s := fixture(t, prefix+ending, true, false)
			if after {
				_, _ = s.Next()
			}
			if ev, err := s.Next(); err != nil || ev.Kind != gateway.EventError || ev.Err == nil {
				t.Fatalf("error = %+v %v", ev, err)
			}
		}
	}
	s := fixture(t, "event: add\ndata: hello\n\nevent: finish\n\n", true, true)
	for i := 0; i < 3; i++ {
		ev, err := s.Next()
		if err != nil || ev.Usage != nil || (i == 2 && ev.Kind != gateway.EventDone) {
			t.Fatalf("no usage = %+v %v", ev, err)
		}
	}
	s = fixture(t, "event: finish\nmeta: {\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":0}}\n\n", true, true)
	for i := 0; i < 2; i++ {
		ev, err := s.Next()
		if err != nil || ev.Kind == gateway.EventData || (i == 1 && ev.Kind != gateway.EventDone) {
			t.Fatalf("empty leaked data = %+v %v", ev, err)
		}
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadErrorsCancelTimeoutAndOversizePropagate(t *testing.T) {
	for _, readErr := range []error{io.ErrClosedPipe, context.Canceled, context.DeadlineExceeded} {
		for _, prefix := range []string{"", "event: add\ndata: delivered\n\n"} {
			s := (Provider{}).Decode(&gateway.Request{Stream: true}, &http.Response{Body: io.NopCloser(io.MultiReader(strings.NewReader(prefix), failingReader{err: readErr}))})
			if prefix != "" {
				_, _ = s.Next()
			}
			if _, err := s.Next(); !errors.Is(err, readErr) {
				t.Fatalf("read error = %v want %v", err, readErr)
			}
			_ = s.Close()
		}
	}
	s := fixture(t, "event: add\ndata: "+strings.Repeat("x", maxEvent)+"\n\n", true, false)
	if _, err := s.Next(); !errors.Is(err, sse.ErrEventTooLarge) {
		t.Fatalf("oversized event = %v", err)
	}
	for _, usage := range []string{`{"prompt_tokens":-1,"completion_tokens":0}`, `{"prompt_tokens":1.5,"completion_tokens":0}`, `{"prompt_tokens":"1","completion_tokens":0}`, `{"prompt_tokens":9223372036854775807,"completion_tokens":1}`} {
		s := fixture(t, "event: finish\nmeta: {\"usage\":"+usage+"}\n\n", true, false)
		if _, err := s.Next(); err == nil {
			t.Fatalf("invalid usage accepted: %s", usage)
		}
	}
}
