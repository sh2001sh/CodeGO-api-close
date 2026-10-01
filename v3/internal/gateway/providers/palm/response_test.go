package palm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestFullAndEmulatedStreamingResponsesPreserveAllCandidates(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "stream"}[streaming], func(t *testing.T) {
			req := chatRequest()
			req.Stream = streaming
			req.Received = time.Unix(1234, 0)
			body := &observedBody{Reader: strings.NewReader(`{"candidates":[{"author":"bot","content":"Hello"},{"author":"bot","content":"你好"}]}`)}
			stream := (Provider{}).Decode(req, &http.Response{Body: body})
			event, err := stream.Next()
			if err != nil || event.Kind != gateway.EventData || event.Usage != nil || event.TextBytes != 11 {
				t.Fatalf("event = %#v, err = %v", event, err)
			}
			var out chatResponse
			if err := json.Unmarshal(event.Payload, &out); err != nil {
				t.Fatal(err)
			}
			if out.ID != req.ID || out.Created != 1234 || out.Model != "PaLM-2" || len(out.Choices) != 2 {
				t.Fatalf("response metadata = %#v", out)
			}
			if strings.Contains(string(event.Payload), `"usage"`) {
				t.Fatal("fabricated provider usage")
			}
			fixture := `{"id":"chatcmpl-fixture","object":"chat.completion","created":1234,"model":"PaLM-2","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"},{"index":1,"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}]}`
			if streaming {
				fixture = `{"id":"chatcmpl-fixture","object":"chat.completion.chunk","created":1234,"model":"PaLM-2","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":"stop"},{"index":1,"delta":{"role":"assistant","content":"你好"},"finish_reason":"stop"}]}`
			}
			assertJSONFixture(t, event.Payload, fixture)
			for i, content := range []string{"Hello", "你好"} {
				choice := out.Choices[i]
				message := choice.Message
				if streaming {
					message = choice.Delta
				}
				if choice.Index != i || choice.FinishReason != "stop" || message == nil || message.Content != content || message.Role != "assistant" {
					t.Fatalf("choice %d = %#v", i, choice)
				}
			}
			object := "chat.completion"
			if streaming {
				object += ".chunk"
			}
			if out.Object != object {
				t.Fatal("wrong client response object")
			}
			if done, err := stream.Next(); err != nil || done.Kind != gateway.EventDone {
				t.Fatalf("terminal = %#v, err = %v", done, err)
			}
			if _, err := stream.Next(); !errors.Is(err, io.EOF) {
				t.Fatalf("terminal not stable: %v", err)
			}
			if err := stream.Close(); err != nil || !body.closed {
				t.Fatal("response body was not closed")
			}
		})
	}
}

func TestNativeErrorAndEmptyResponsesNeverEmitDone(t *testing.T) {
	for _, test := range []struct {
		body, code string
		status     int
	}{
		{`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"rate limited"}}`, "429", 429},
		{`{"error":{"code":13,"status":"INTERNAL","message":"broken"}}`, "13", 502},
		{`{"error":{}}`, "0", 502},
		{`{"candidates":[],"filters":[{"reason":"SAFETY","message":"blocked"}]}`, "content_filter", 502},
		{`{"candidates":[]}`, "empty_response", 502},
		{`{"candidates":[{"content":""}]}`, "empty_response", 502},
		{`{}`, "empty_response", 502},
	} {
		for _, streaming := range []bool{false, true} {
			req := chatRequest()
			req.Stream = streaming
			stream := (Provider{}).Decode(req, &http.Response{Body: io.NopCloser(strings.NewReader(test.body))})
			event, err := stream.Next()
			if err != nil || event.Kind != gateway.EventError || event.Err == nil || event.Err.Code != test.code || event.Err.Status != test.status || event.Usage != nil {
				t.Fatalf("body %s: event = %#v, err = %v", test.body, event, err)
			}
			if _, err := stream.Next(); !errors.Is(err, io.EOF) {
				t.Fatalf("error followed by false success: %v", err)
			}
			_ = stream.Close()
		}
	}
}

func TestMalformedAndTruncatedNativeResponseIsFailureBeforeOutput(t *testing.T) {
	for _, body := range []string{"", "{", "[]", `{"candidates":[{"content":1}]}`, `{"candidates":[{"content":"partial"}]`, `{"error":{"code":"429"}}`} {
		stream := (Provider{}).Decode(chatRequest(), &http.Response{Body: io.NopCloser(strings.NewReader(body))})
		event, err := stream.Next()
		if err == nil || event.Kind == gateway.EventData || len(event.Payload) != 0 {
			t.Fatalf("accepted malformed response %q: %#v %v", body, event, err)
		}
		_ = stream.Close()
	}
}

func TestReadCancelTimeoutAndCloseFailuresRemainObservable(t *testing.T) {
	for _, want := range []error{io.ErrUnexpectedEOF, context.Canceled, context.DeadlineExceeded} {
		stream := (Provider{}).Decode(chatRequest(), &http.Response{Body: &observedBody{readErr: want}})
		if _, err := stream.Next(); !errors.Is(err, want) {
			t.Fatalf("lost read failure: got %v want %v", err, want)
		}
		_ = stream.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://invalid.test", nil)
	body := &observedBody{Reader: strings.NewReader(`{"candidates":[{"content":"unexpected"}]}`), closeErr: io.ErrClosedPipe}
	stream := (Provider{}).Decode(chatRequest(), &http.Response{Body: body, Request: request})
	if _, err := stream.Next(); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost request cancellation: %v", err)
	}
	if err := stream.Close(); !errors.Is(err, io.ErrClosedPipe) || !body.closed {
		t.Fatal("lost Close failure")
	}
}

func TestHTTPBodyDeadlineDoesNotProducePartialEmulatedStream(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":"partial`)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req := chatRequest()
	req.Stream = true
	request, err := (Provider{}).BuildRequest(ctx, req, gateway.Target{BaseURL: server.URL, Secret: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	stream := (Provider{}).Decode(req, response)
	defer func() {
		if err := stream.Close(); err != nil {
			t.Error(err)
		}
	}()
	event, err := stream.Next()
	if !errors.Is(err, context.DeadlineExceeded) || event.Kind == gateway.EventData {
		t.Fatalf("timeout exposed partial content: %#v %v", event, err)
	}
}

type observedBody struct {
	io.Reader
	readErr, closeErr error
	closed            bool
}

func (b *observedBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	return b.Reader.Read(p)
}

func (b *observedBody) Close() error {
	b.closed = true
	return b.closeErr
}
