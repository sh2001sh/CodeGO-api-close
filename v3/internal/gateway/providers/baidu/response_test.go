package baidu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func decodeFixture(stream bool, body, contentType, requestBody string) gateway.EventStream {
	return (Provider{}).Decode(&gateway.Request{Model: "public-model", Stream: stream, Body: []byte(requestBody)}, &http.Response{
		Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{contentType}}, StatusCode: 200})
}

func TestNativeSingleResponseAndHonestMissingUsage(t *testing.T) {
	for _, fixture := range []struct {
		body  string
		count int64
	}{
		{`{"id":"b1","created":7,"result":"hello","usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`, 3},
		{`{"id":"b1","created":7,"result":"hello","is_truncated":true,"usage":{"prompt_tokens":4,"total_tokens":7}}`, 3},
		{`{"id":"b1","created":7,"result":"hello"}`, -1},
		{`{"id":"b1","created":7,"result":"hello","usage":{}}`, -1},
	} {
		s := decodeFixture(false, fixture.body, "application/json", "{}")
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventData || ev.TextBytes != 5 || gjson.GetBytes(ev.Payload, "model").Str != "public-model" || gjson.GetBytes(ev.Payload, "id").Str != "b1" || gjson.GetBytes(ev.Payload, "created").Int() != 7 || gjson.GetBytes(ev.Payload, "choices.0.message.content").Str != "hello" {
			t.Fatalf("response conversion failed: %+v %v", ev, err)
		}
		if fixture.count < 0 && ev.Usage != nil {
			t.Fatal("missing upstream usage must remain nil")
		}
		if fixture.count >= 0 && (ev.Usage == nil || ev.Usage.PromptTokens != 4 || ev.Usage.CompletionTokens != fixture.count) {
			t.Fatalf("usage incorrect: %+v", ev.Usage)
		}
		if _, err := s.Next(); !errors.Is(err, io.EOF) {
			t.Fatalf("single response did not end: %v", err)
		}
		_ = s.Close()
	}
}

func TestStreamFinishUsageAndEmptyGating(t *testing.T) {
	body := "data: " + `{"id":"b1","result":"hi"}` + "\n\n" + "data: " + `{"result":"!","is_end":true,"usage":{"prompt_tokens":8,"total_tokens":10}}` + "\n\n"
	for _, wantUsage := range []bool{false, true} {
		requestBody := `{}`
		if wantUsage {
			requestBody = `{"stream_options":{"include_usage":true}}`
		}
		s := decodeFixture(true, body, "text/event-stream", requestBody)
		for i, text := range []string{"hi", "!"} {
			ev, err := s.Next()
			if err != nil || ev.Kind != gateway.EventData || ev.TextBytes != len(text) || gjson.GetBytes(ev.Payload, "choices.0.delta.content").Str != text {
				t.Fatalf("chunk lost: %+v %v", ev, err)
			}
			if i == 0 && gjson.GetBytes(ev.Payload, "choices.0.delta.role").Str != "assistant" {
				t.Fatal("first assistant role missing")
			}
			if i == 1 && (ev.Usage == nil || ev.Usage.CompletionTokens != 2 || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str != "stop") {
				t.Fatalf("last chunk lost finish/usage: %+v", ev)
			}
			if gjson.GetBytes(ev.Payload, "usage").Exists() {
				t.Fatal("usage must be a separate requested chunk")
			}
		}
		if wantUsage {
			ev, err := s.Next()
			if err != nil || ev.Kind != gateway.EventData || len(gjson.GetBytes(ev.Payload, "choices").Array()) != 0 || gjson.GetBytes(ev.Payload, "usage.total_tokens").Int() != 10 {
				t.Fatalf("usage chunk missing: %+v %v", ev, err)
			}
		}
		if ev, err := s.Next(); err != nil || ev.Kind != gateway.EventDone {
			t.Fatalf("clean terminal missing: %+v %v", ev, err)
		}
		_ = s.Close()
	}
	for _, body := range []string{"", "data: " + `{"result":"","is_end":true,"usage":{"prompt_tokens":8,"total_tokens":8}}` + "\n\n"} {
		s := decodeFixture(true, body, "text/event-stream", "{}")
		for {
			ev, err := s.Next()
			if err == io.EOF || ev.Kind == gateway.EventDone {
				break
			}
			if err != nil || ev.Kind == gateway.EventData {
				t.Fatalf("empty stream became output: %+v %v", ev, err)
			}
		}
		_ = s.Close()
	}
}

func TestStreamErrorsBeforeAfterOutputAndTruncation(t *testing.T) {
	for _, before := range []bool{false, true} {
		prefix := ""
		if !before {
			prefix = "data: " + `{"result":"partial"}` + "\n\n"
		}
		s := decodeFixture(true, prefix+"data: "+`{"error_code":18,"error_msg":"limit reached"}`+"\n\n", "text/event-stream", "{}")
		if !before {
			if ev, err := s.Next(); err != nil || ev.TextBytes != 7 {
				t.Fatalf("partial output missing: %+v %v", ev, err)
			}
		}
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventError || ev.Err.Code != "baidu_18" || ev.Err.Message != "limit reached" {
			t.Fatalf("in-band error lost: %+v %v", ev, err)
		}
		_ = s.Close()
	}
	for _, tail := range []string{"", "data: {"} {
		s := decodeFixture(true, "data: "+`{"result":"partial"}`+"\n\n"+tail, "text/event-stream", "{}")
		_, _ = s.Next()
		if _, err := s.Next(); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("truncated stream accepted as successful: %v", err)
		}
		_ = s.Close()
	}
}

type readFailure struct{ cause error }

func (r readFailure) Read([]byte) (int, error) { return 0, r.cause }

func TestReadCancellationAndDeadlineArePreserved(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, io.ErrClosedPipe} {
		for _, stream := range []bool{false, true} {
			header := http.Header{}
			if stream {
				header.Set("Content-Type", "text/event-stream")
			}
			s := (Provider{}).Decode(&gateway.Request{Stream: stream}, &http.Response{Header: header, Body: io.NopCloser(readFailure{cause})})
			if _, err := s.Next(); !errors.Is(err, cause) {
				t.Fatalf("read failure replaced: %v", err)
			}
			_ = s.Close()
		}
	}
}

func TestMalformedResponseAndUsageFailExplicitly(t *testing.T) {
	for _, body := range []string{`{`, `[]`, `{"result":4}`, `{"result":"hi","usage":7}`, `{"result":"hi","usage":{"prompt_tokens":3,"total_tokens":1}}`, `{"result":"hi","usage":{"prompt_tokens":-1,"completion_tokens":2}}`,
		`{"result":"hi","usage":{"prompt_tokens":9223372036854775808,"completion_tokens":2}}`,
		`{"result":"hi","usage":{"prompt_tokens":9223372036854775807,"completion_tokens":2}}`,
		`{"result":"hi","usage":{"prompt_tokens":1,"completion_tokens":1.5}}`} {
		s := decodeFixture(false, body, "application/json", "{}")
		ev, err := s.Next()
		_ = s.Close()
		if err == nil && ev.Kind != gateway.EventError {
			t.Fatalf("malformed response accepted: %s => %+v", body, ev)
		}
	}
}
