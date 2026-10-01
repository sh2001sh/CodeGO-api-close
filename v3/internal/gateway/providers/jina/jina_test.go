package jina

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestNativeRequestsPreservePayloads(t *testing.T) {
	for _, test := range []struct {
		name, path, body string
		build            func(context.Context, *gateway.Request, gateway.Target) (*http.Request, error)
	}{
		{"embeddings", "/prefix/v1/embeddings", `{"model":"public","input":[{"image":"https://example.org/image.jpg"},{"text":"hello"}],"encoding_format":"base64","task":"retrieval.passage","dimensions":128}`, BuildEmbeddingRequest},
		{"rerank", "/prefix/v1/rerank", `{"model":"public","query":"hello","documents":["one",{"text":"two"}],"top_n":1,"return_documents":true}`, BuildRerankRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != test.path || r.Header.Get("Authorization") != "Bearer fake-key" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("wrong native request: method=%s path=%s headers=%v", r.Method, r.URL.Path, r.Header)
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				var model string
				_ = json.Unmarshal(body["model"], &model)
				if model != "upstream" {
					t.Errorf("model %q", model)
				}
				var original map[string]json.RawMessage
				_ = json.Unmarshal([]byte(test.body), &original)
				for key, value := range original {
					if key != "model" && key != "encoding_format" && string(value) != string(body[key]) {
						t.Errorf("field %s changed: got %s want %s", key, body[key], value)
					}
				}
				if _, exists := body["encoding_format"]; exists {
					t.Error("encoding_format retained")
				}
				_, _ = w.Write([]byte(`{"usage":{"total_tokens":4}}`))
			}))
			defer server.Close()
			source := &gateway.Request{Model: "public", Body: []byte(test.body)}
			out, err := test.build(context.Background(), source, gateway.Target{BaseURL: server.URL + "/prefix/v1", Secret: "fake-key", UpstreamModel: "upstream"})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := server.Client().Do(out)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if string(source.Body) != test.body {
				t.Error("source mutated")
			}
		})
	}
}

func TestInvalidNativeRequests(t *testing.T) {
	target := gateway.Target{BaseURL: "https://api.jina.ai", Secret: "fake-key"}
	for _, body := range []string{`null`, `[]`, `{`, `{}`, `{"input":null}`} {
		if _, err := BuildEmbeddingRequest(context.Background(), &gateway.Request{Model: "m", Body: []byte(body)}, target); err == nil {
			t.Errorf("accepted embedding body %s", body)
		}
	}
	for _, body := range []string{`{}`, `{"query":"","documents":["x"]}`, `{"query":"x","documents":[]}`, `{"query":"x","documents":"x"}`} {
		if _, err := BuildRerankRequest(context.Background(), &gateway.Request{Model: "m", Body: []byte(body)}, target); err == nil {
			t.Errorf("accepted rerank body %s", body)
		}
	}
	req := &gateway.Request{Model: "m", Body: []byte(`{"input":"hello"}`)}
	for _, invalidTarget := range []gateway.Target{
		{BaseURL: "https://api.jina.ai"},
		{BaseURL: "file:///tmp/provider", Secret: "fake-key"},
		{BaseURL: "https://user:pass@example.org", Secret: "fake-key"},
		{BaseURL: "https://api.jina.ai?path=x", Secret: "fake-key"},
	} {
		if _, err := BuildEmbeddingRequest(context.Background(), req, invalidTarget); err == nil {
			t.Errorf("accepted invalid target %q", invalidTarget.BaseURL)
		} else {
			var upstream *gateway.UpstreamError
			if !errors.As(err, &upstream) || upstream.Status != http.StatusBadGateway {
				t.Errorf("channel configuration must allow failover, got %v", err)
			}
		}
	}
}

func TestChatIsExplicitlyUnsupported(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolOpenAIChat, gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		_, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: protocol}, gateway.Target{})
		var upstream *gateway.UpstreamError
		if !errors.As(err, &upstream) || upstream.Status != 400 || upstream.Code != "unsupported_protocol" {
			t.Fatalf("protocol %d: %v", protocol, err)
		}
	}
	s := (Provider{}).Decode(&gateway.Request{}, &http.Response{Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"should not leak"}}]}`))})
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError || len(ev.Payload) != 0 || ev.Usage != nil {
		t.Fatalf("unexpected chat event: %#v, %v", ev, err)
	}
	if _, err = s.Next(); err != io.EOF {
		t.Fatalf("after error: %v", err)
	}
	_ = s.Close()
}

func TestNativeRequestHonorsCancellation(t *testing.T) {
	for _, expired := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if expired {
			var deadlineCancel context.CancelFunc
			ctx, deadlineCancel = context.WithTimeout(context.Background(), -1)
			defer deadlineCancel()
		} else {
			cancel()
		}
		defer cancel()
		req, err := BuildEmbeddingRequest(ctx, &gateway.Request{Model: "m", Body: []byte(`{"input":"hello"}`)}, gateway.Target{BaseURL: "https://api.jina.ai", Secret: "fake-key"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = http.DefaultClient.Do(req)
		if !errors.Is(err, ctx.Err()) {
			t.Fatalf("context error %v, got %v", ctx.Err(), err)
		}
	}
}
