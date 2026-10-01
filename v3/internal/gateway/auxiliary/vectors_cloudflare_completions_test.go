package auxiliary

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
	"github.com/tidwall/gjson"
)

func TestCloudflareCompletionsNativeWire(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "stream"}[stream], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/client/v4/accounts/account/ai/run/@cf/meta/llama" || r.Header.Get("Authorization") != "Bearer provider-key" {
					t.Errorf("bad completions wire request: %s %s", r.Method, r.URL.Path)
				}
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				for field, want := range map[string]string{"prompt": "Hello", "max_tokens": "17", "stream": map[bool]string{false: "false", true: "true"}[stream], "temperature": "0.4"} {
					if gjson.GetBytes(data, field).String() != want {
						t.Errorf("%s mismatch: %s", field, data)
					}
				}
				for _, field := range []string{"model", "max_completion_tokens", "stream_options", "n", "echo"} {
					if gjson.GetBytes(data, field).Exists() {
						t.Errorf("unexpected native field %s: %s", field, data)
					}
				}
				if stream && r.Header.Get("Accept") != "text/event-stream" {
					t.Error("native streaming Accept missing")
				}
				w.WriteHeader(200)
			}))
			defer server.Close()
			body, _ := json.Marshal(map[string]any{"model": "alias", "prompt": "Hello", "max_tokens": 8, "max_completion_tokens": 17, "stream": stream, "temperature": 0.4, "n": 1, "echo": false, "stream_options": map[string]bool{"include_usage": true}})
			req := &gateway.Request{Model: "alias", Stream: stream, Body: body}
			wire, err := vectorAdapters()["cloudflare"].Build(context.Background(), req, gateway.Target{BaseURL: server.URL, Secret: "account|provider-key", UpstreamModel: "@cf/meta/llama"}, Input{Operation: Completions, Body: body})
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(wire)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
		})
	}
}

func TestCloudflareCompletionsNormalizesTextAndActualUsage(t *testing.T) {
	response, err := vectorAdapters()["cloudflare"].Decode(context.Background(), &gateway.Request{ID: "test", Model: "alias"}, gateway.Target{}, Input{Operation: Completions}, vectorTestResponse(`{"success":true,"result":{"response":"generated text","usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}}`, 200))
	if err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{"id": "cmpl-test", "object": "text_completion", "model": "alias", "choices.0.text": "generated text", "choices.0.finish_reason": "stop", "usage.total_tokens": "5"} {
		if got := gjson.GetBytes(response.Body, field).String(); got != want {
			t.Errorf("%s=%q want=%q; %s", field, got, want, response.Body)
		}
	}
	if gjson.GetBytes(response.Body, "choices.0.message").Exists() || response.Usage == nil || response.Usage.CompletionTokens != 2 || response.Usage.Estimated {
		t.Errorf("completions result=%s usage=%+v", response.Body, response.Usage)
	}
	if response.Header.Get("Content-Length") != "" || response.Header.Get("Content-Encoding") != "" {
		t.Error("stale completion response headers")
	}
}

func TestCloudflareCompletionsRejectsLossyRequests(t *testing.T) {
	for _, body := range []string{`{}`, `{"prompt":[]}`, `{"prompt":""}`, `{"prompt":"text","n":2}`, `{"prompt":"text","echo":true}`, `{"prompt":"text","logprobs":1}`, `{"prompt":"text","stop":"end"}`, `{"prompt":"text","top_p":0.8}`, `{"prompt":"text","max_tokens":-1}`, `{"prompt":"text","max_tokens":1.5}`, `{"prompt":"text","temperature":"hot"}`, `{"prompt":"text","stream_options":{"unsupported":true}}`} {
		_, err := vectorAdapters()["cloudflare"].Build(context.Background(), &gateway.Request{Model: "@cf/model"}, gateway.Target{BaseURL: "https://api.example", Secret: "account|key"}, Input{Operation: Completions, Body: []byte(body)})
		if err == nil {
			t.Errorf("accepted lossy/malformed completions request: %s", body)
		}
	}
}

type cloudflareCompletionStreamDecoder interface {
	DecodeStream(*gateway.Request, gateway.Target, Input, *http.Response) gateway.EventStream
}

func TestCloudflareCompletionsNativeStreamConversion(t *testing.T) {
	decoder, ok := vectorAdapters()["cloudflare"].(cloudflareCompletionStreamDecoder)
	if !ok {
		t.Fatal("completions native stream decoder missing")
	}
	req := &gateway.Request{ID: "stream", Model: "alias", Stream: true, Body: []byte(`{"stream_options":{"include_usage":true}}`)}
	stream := decoder.DecodeStream(req, gateway.Target{}, Input{Operation: Completions}, vectorTestResponse("data: {\"response\":\"Hello\"}\n\ndata: {\"response\":\" world\"}\n\ndata: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n", 200))
	defer func() { _ = stream.Close() }()
	var text strings.Builder
	done, usage, finish := false, false, false
	for {
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Kind == gateway.EventError {
			t.Fatal(event.Err)
		}
		if event.Usage != nil {
			usage = event.Usage.CompletionTokens == 2 && !event.Usage.Estimated
		}
		if event.Kind == gateway.EventDone {
			done = true
		}
		if event.Kind == gateway.EventData {
			if gjson.GetBytes(event.Payload, "object").String() != "text_completion" || gjson.GetBytes(event.Payload, "id").String() != "cmpl-stream" || gjson.GetBytes(event.Payload, "choices.0.delta").Exists() {
				t.Errorf("stream did not convert native response: %s", event.Payload)
			}
			text.WriteString(gjson.GetBytes(event.Payload, "choices.0.text").String())
			if gjson.GetBytes(event.Payload, "choices.0.finish_reason").String() == "stop" {
				finish = true
			}
		}
	}
	if text.String() != "Hello world" || !done || !usage || !finish {
		t.Fatalf("text=%q done=%v usage=%v finish=%v", text.String(), done, usage, finish)
	}
}

func TestCloudflareCompletionsStreamCutAndNativeErrors(t *testing.T) {
	decoder, ok := vectorAdapters()["cloudflare"].(cloudflareCompletionStreamDecoder)
	if !ok {
		t.Fatal("native stream decoder missing")
	}
	for _, body := range []string{"data: {\"response\":\"partial\"}\n\n", "data: {\"success\":false,\"errors\":[{\"message\":\"failure\"}]}\n\n", "data: {\"response\":42}\n\ndata: [DONE]\n\n", "data: [DONE]\n\n", "data: {\"response\":\"text\"}\n\ndata: {\"usage\":{\"prompt_tokens\":-1,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"} {
		stream := decoder.DecodeStream(&gateway.Request{Stream: true}, gateway.Target{}, Input{Operation: Completions}, vectorTestResponse(body, 200))
		failed, done := false, false
		for {
			event, err := stream.Next()
			if err != nil {
				failed = failed || !errors.Is(err, io.EOF)
				break
			}
			if event.Kind == gateway.EventError {
				failed = true
			}
			if event.Kind == gateway.EventDone {
				done = true
			}
		}
		_ = stream.Close()
		if !failed || done {
			t.Errorf("native malformed stream treated as clean: %q failed=%v done=%v", body, failed, done)
		}
	}
	if decoder.DecodeStream(&gateway.Request{}, gateway.Target{}, Input{Operation: Embeddings}, nil) != nil {
		t.Error("claimed native stream conversion for embeddings")
	}
}
