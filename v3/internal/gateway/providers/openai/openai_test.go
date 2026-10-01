package openai

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

func TestBuildRequestModelAndVersionPrefix(t *testing.T) {
	req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "public", Stream: true, Body: []byte(`{"model":"public","stream":true,"messages":[]}`)}
	r, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{BaseURL: "https://upstream.example/v1", Secret: "test-key", UpstreamModel: "private"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" || gjson.GetBytes(body, "model").Str != "private" || !gjson.GetBytes(body, "stream_options.include_usage").Bool() {
		t.Fatalf("bad upstream request path=%s body=%s", r.URL.Path, body)
	}
	if gjson.GetBytes(req.Body, "model").Str != "public" {
		t.Fatal("modified original request")
	}
}

func decoder(body string) gateway.EventStream {
	resp := &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	return (Provider{}).Decode(&gateway.Request{Body: []byte(`{}`), Stream: true}, resp)
}

func TestFirstSemanticEventGate(t *testing.T) {
	role := `data: {"choices":[{"index":0,"delta":{"role":"assistant"}}]}` + "\n\n"
	content := `data: {"choices":[{"index":0,"delta":{"content":"hello"}}]}` + "\n\n"
	errorData := `data: {"error":{"message":"unavailable","code":"overloaded"}}` + "\n\n"
	s := decoder(role + errorData)
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError {
		t.Fatalf("role leaked before error: %+v %v", ev, err)
	}
	s = decoder(role + "data: [DONE]\n\n")
	ev, err = s.Next()
	if err != nil || ev.Kind != gateway.EventDone {
		t.Fatalf("lifecycle-only stream committed: %+v %v", ev, err)
	}
	s = decoder(role + content + "data: [DONE]\n\n")
	for _, want := range []string{"role", "hello"} {
		ev, err = s.Next()
		if err != nil || !strings.Contains(string(ev.Payload), want) {
			t.Fatalf("gate order=%s err=%v want=%s", ev.Payload, err, want)
		}
	}
}

func TestPrematureEOFAndFinalFinishReason(t *testing.T) {
	for _, tc := range []struct {
		tail string
		want error
	}{
		{"", io.ErrUnexpectedEOF},
		{"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", nil},
	} {
		s := decoder("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n" + tc.tail)
		_, _ = s.Next()
		for {
			ev, err := s.Next()
			if err != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("err=%v want=%v", err, tc.want)
				}
				break
			}
			if ev.Kind == gateway.EventDone {
				if tc.want != nil {
					t.Fatal("premature EOF was marked done")
				}
				break
			}
		}
	}
}

func TestMalformedStreamAndToolEstimate(t *testing.T) {
	if _, err := decoder("data: {invalid}\n\n").Next(); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	s := decoder("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"id\":\"call1\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"q\\\":1}\"}}]}}]}\n\ndata: [DONE]\n\n")
	ev, err := s.Next()
	if err != nil || ev.TextBytes != 7 {
		t.Fatalf("tool estimate=%d err=%v", ev.TextBytes, err)
	}
}
