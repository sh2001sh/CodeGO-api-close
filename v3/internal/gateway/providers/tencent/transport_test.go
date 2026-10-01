package tencent

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
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

func TestTC3SignatureMatchesIndependentReferenceVector(t *testing.T) {
	// Expected digest was independently calculated with Python hashlib/hmac,
	// using Tencent's published TC3 canonical-request and key derivation steps.
	body := []byte(`{"Model":"hunyuan-lite","Messages":[{"Role":"user","Content":"hello"}],"Stream":true,"Temperature":0}`)
	req, err := http.NewRequest(http.MethodPost, "https://hunyuan.tencentcloudapi.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TC-Action", "ChatCompletions")
	got := signature(req, body, "test-id", "test-secret", 1700000000)
	want := "TC3-HMAC-SHA256 Credential=test-id/2023-11-14/hunyuan/tc3_request, SignedHeaders=content-type;host;x-tc-action, Signature=01e43b83c6efd68dfec4f5d085885a6ab213c56484855976bc0f11ae1f04a305"
	if got != want {
		t.Fatalf("TC3 signature mismatch: %s", got)
	}
	changed := signature(req, append(body, ' '), "test-id", "test-secret", 1700000000)
	if changed == got {
		t.Fatal("signature does not bind exact request bytes")
	}
}

func TestSignedNativeHTTPReplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/" || !strings.Contains(string(body), `"Messages":[{"Role":"user","Content":"hello"}]`) || !strings.HasPrefix(r.Header.Get("Authorization"), "TC3-HMAC-SHA256 ") {
			t.Errorf("incorrect native request: %s %s", r.URL, body)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, event(`{"Choices":[{"Delta":{"Content":"hello"}}]}`)+event(`{"Choices":[{"Delta":{"Content":""},"FinishReason":"stop"}],"Usage":{"PromptTokens":1,"CompletionTokens":2}}`))
	}))
	defer server.Close()
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hello"}]}`, true)
	out, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "123|test-id|test-secret", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(out)
	if err != nil {
		t.Fatal(err)
	}
	s := (Provider{}).Decode(req, resp)
	defer func() { _ = s.Close() }()
	for _, expected := range []gateway.EventKind{gateway.EventData, gateway.EventData, gateway.EventDone} {
		ev, err := s.Next()
		if err != nil || ev.Kind != expected {
			t.Fatalf("native replay failed: %+v %v", ev, err)
		}
		if ev.Usage != nil && (ev.Usage.PromptTokens != 1 || ev.Usage.CompletionTokens != 2) {
			t.Fatalf("wrong native billing usage: %+v", ev.Usage)
		}
	}
}

func TestHTTPBodyReadPreservesClientCancelAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, event(`{"Choices":[{"Delta":{"Role":"assistant"}}]}`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	for _, mode := range []string{"cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			want := context.Canceled
			if mode == "cancel" {
				ctx, cancel = context.WithCancel(context.Background())
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
				want = context.DeadlineExceeded
			}
			defer cancel()
			req := fixtureRequest(`{"messages":[{"role":"user","content":"hello"}]}`, true)
			out, err := (Provider{}).BuildRequest(ctx, req, gateway.Target{Secret: "123|test-id|test-secret", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := server.Client().Do(out)
			if err != nil {
				if mode == "timeout" && errors.Is(err, context.DeadlineExceeded) {
					return
				}
				t.Fatal(err)
			}
			s := (Provider{}).Decode(req, resp)
			defer func() { _ = s.Close() }()
			if mode == "cancel" {
				cancel()
			}
			if ev, err := s.Next(); !errors.Is(err, want) {
				t.Fatalf("cancel/timeout not preserved: %+v %v", ev, err)
			}
		})
	}
}

func TestStreamErrorEnvelopeAndUnexpectedJSONContentType(t *testing.T) {
	for _, data := range []string{
		`{"Response":{"Error":{"Code":"AuthFailure","Message":"bad key"}}}`,
		`{"Response":{"Choices":[{"Message":{"Content":"hello"},"FinishReason":"stop"}]}}`,
	} {
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(data)), Header: http.Header{"Content-Type": {"application/json"}}}
		s := (Provider{}).Decode(fixtureRequest(`{}`, true), resp)
		ev, err := s.Next()
		_ = s.Close()
		if err != nil || ev.Kind != gateway.EventError {
			t.Fatalf("unexpected JSON became streaming success: %+v %v", ev, err)
		}
	}
	s := fixtureStream(true, event(`{"Error":{"Code":"LimitExceeded","Message":"try later"}}`), false)
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError || ev.Err.Code != "LimitExceeded" {
		t.Fatalf("stream error lost: %+v %v", ev, err)
	}
}

func TestOversizedEventAndPostFinishOutputAreRejected(t *testing.T) {
	s := fixtureStream(true, "data: "+strings.Repeat("x", sse.DefaultMaxEventSize+1)+"\n\n", false)
	if _, err := s.Next(); !errors.Is(err, sse.ErrEventTooLarge) {
		t.Fatalf("unbounded event accepted: %v", err)
	}
	_ = s.Close()
	s = fixtureStream(true, event(`{"Choices":[{"Delta":{"Content":"hello"},"FinishReason":"stop"}]}`)+event(`{"Choices":[{"Delta":{"Content":"extra"}}]}`), false)
	defer func() { _ = s.Close() }()
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Next(); err == nil {
		t.Fatal("output after finish accepted")
	}
}

func TestNativeResponseMustMatchSingleRequestedChoice(t *testing.T) {
	for _, data := range []string{
		`{"Choices":[{"Index":123,"Delta":{"Content":"hello"}}]}`,
		`{"Choices":[{"Delta":{"Content":"hello"}},{"Delta":{"Content":"extra"}}]}`,
	} {
		s := fixtureStream(true, event(data), false)
		_, err := s.Next()
		_ = s.Close()
		if err == nil {
			t.Fatalf("unexpected choices accepted: %s", data)
		}
	}
}
