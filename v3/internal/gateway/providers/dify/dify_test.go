package dify

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func chatRequest(body string, stream bool) *gateway.Request {
	return &gateway.Request{ID: "request-id", Received: time.Unix(100, 0), Protocol: gateway.ProtocolOpenAIChat,
		Model: "app-alias", Stream: stream, Body: []byte(body)}
}

func TestNativeRequestAuthAndHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tenant/v1/chat-messages" {
			t.Errorf("unexpected native route: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing native authentication/content type")
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Error("streaming request must accept SSE")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		root := gjson.ParseBytes(body)
		if root.Get("query").Str != "SYSTEM: \nBe precise\nASSISTANT: \nPrevious answer\nUSER: \nFollow up\n" {
			t.Errorf("history was lost: %s", root.Get("query").Str)
		}
		for key, expected := range map[string]string{"user": "customer-a", "conversation_id": "conversation-a", "response_mode": "streaming", "inputs.topic": "finance", "files.0.type": "image", "files.0.transfer_mode": "remote_url", "files.0.url": "https://images.example/figure.png"} {
			if root.Get(key).Str != expected {
				t.Errorf("%s = %s, want %s", key, root.Get(key).Str, expected)
			}
		}
		if root.Get("auto_generate_name").Bool() || root.Get("model").Exists() {
			t.Errorf("Dify app settings must not be replaced: %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"event\":\"message\",\"message_id\":\"msg\",\"answer\":\"Hello\"}\n\ndata: {\"event\":\"message_end\",\"metadata\":{\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}}\n\n")
	}))
	defer server.Close()
	req := chatRequest(`{"model":"app-alias","messages":[{"role":"system","content":"Be precise"},{"role":"assistant","content":"Previous answer"},{"role":"user","content":[{"type":"text","text":"Follow up"},{"type":"image_url","image_url":{"url":"https://images.example/figure.png","detail":"auto"}}]}],"inputs":{"topic":"finance"},"user":"customer-a","conversation_id":"conversation-a","stream_options":{"include_usage":true}}`, true)
	out, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{BaseURL: server.URL + "/tenant/v1/", Secret: "test-token", UpstreamModel: "must-not-be-forwarded"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(out)
	if err != nil {
		t.Fatal(err)
	}
	stream := (Provider{}).Decode(req, resp)
	defer func() { _ = stream.Close() }()
	data, err := stream.Next()
	if err != nil || data.Kind != gateway.EventData || data.TextBytes != 5 || gjson.GetBytes(data.Payload, "choices.0.delta.content").Str != "Hello" {
		t.Fatalf("bad answer: %+v, %v", data, err)
	}
	finish, err := stream.Next()
	if err != nil || gjson.GetBytes(finish.Payload, "choices.0.finish_reason").Str != "stop" {
		t.Fatalf("bad finish: %+v, %v", finish, err)
	}
	usage, err := stream.Next()
	if err != nil || usage.Kind != gateway.EventData || usage.Usage == nil || usage.Usage.PromptTokens != 4 || usage.Usage.CompletionTokens != 2 || len(gjson.GetBytes(usage.Payload, "choices").Array()) != 0 || gjson.GetBytes(usage.Payload, "usage.total_tokens").Int() != 6 {
		t.Fatalf("bad usage: %+v, %v", usage, err)
	}
	done, err := stream.Next()
	if err != nil || done.Kind != gateway.EventDone {
		t.Fatalf("missing terminal: %+v %v", done, err)
	}
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("after terminal = %v", err)
	}
}

func TestRequestDefaultsAndConversation(t *testing.T) {
	req := chatRequest(`{"messages":[{"role":"user","content":"question"}]}`, false)
	for _, base := range []string{"https://api.example", "https://api.example/v1", "https://api.example/v1/"} {
		out, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{BaseURL: base})
		if err != nil {
			t.Fatal(err)
		}
		if out.URL.String() != "https://api.example/v1/chat-messages" {
			t.Fatal(out.URL)
		}
		body, _ := io.ReadAll(out.Body)
		root := gjson.ParseBytes(body)
		if root.Get("user").Str != req.ID || root.Get("response_mode").Str != "blocking" || !root.Get("inputs").IsObject() || !root.Get("files").IsArray() || root.Get("conversation_id").Exists() {
			t.Fatalf("wrong defaults: %s", body)
		}
	}
}

func TestRejectLossyRequests(t *testing.T) {
	for name, body := range map[string]string{
		"tools":           `{"messages":[{"role":"user","content":"x"}],"tools":[{"type":"function"}]}`,
		"tool history":    `{"messages":[{"role":"tool","content":"x","tool_call_id":"call"}]}`,
		"assistant tools": `{"messages":[{"role":"assistant","content":"x","tool_calls":[]}]}`,
		"inline image":    `{"messages":[{"role":"user","content":[{"type":"text","text":"x"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}]}`,
		"image detail":    `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://images.example/x","detail":"high"}}]}]}`,
		"temperature":     `{"messages":[{"role":"user","content":"x"}],"temperature":0.7}`,
		"max tokens":      `{"messages":[{"role":"user","content":"x"}],"max_tokens":10}`,
		"inputs":          `{"messages":[{"role":"user","content":"x"}],"inputs":"bad"}`,
		"numeric user":    `{"messages":[{"role":"user","content":"x"}],"user":42}`,
		"conversation":    `{"messages":[{"role":"user","content":"x"}],"conversation_id":{}}`,
		"stream option":   `{"messages":[{"role":"user","content":"x"}],"stream_options":{"include_usage":"yes"}}`,
		"empty messages":  `{"messages":[]}`,
		"invalid JSON":    `{"messages":`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := (Provider{}).BuildRequest(context.Background(), chatRequest(body, false), gateway.Target{BaseURL: "https://api.example"})
			var upstream *gateway.UpstreamError
			if !errors.As(err, &upstream) || upstream.Status != http.StatusBadRequest {
				t.Fatalf("expected explicit rejection, got %v", err)
			}
		})
	}
	req := chatRequest(`{"messages":[]}`, false)
	req.Protocol = gateway.ProtocolAnthropic
	if _, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{}); err == nil {
		t.Fatal("accepted unsupported protocol")
	}
}
