package palm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestNativeRequestAuthPayloadAndImmutableInput(t *testing.T) {
	body := []byte(`{"model":"PaLM-2","stream":true,"n":2,"temperature":0,"top_p":0,"top_k":4,"messages":[{"role":"system","content":"Be precise."},{"role":"user","content":[{"type":"text","text":"Hi "},{"type":"text","text":"there"}]},{"role":"assistant","content":"Hello"},{"role":"user","content":"Continue"}],"stream_options":{"include_usage":true}}`)
	original := bytes.Clone(body)
	req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "PaLM-2", Stream: true, Body: body}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1beta2/models/chat-bison-002:generateMessage" {
			t.Errorf("method/path = %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("key") != "fixture key+&" || r.URL.Query().Get("tenant") != "example" {
			t.Error("API key or existing base query not preserved")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
			t.Error("incorrect native request headers")
		}
		actual, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		assertJSONFixture(t, actual, `{"prompt":{"context":"Be precise.","messages":[{"author":"user","content":"Hi there"},{"author":"assistant","content":"Hello"},{"author":"user","content":"Continue"}]},"temperature":0,"candidateCount":2,"topP":0,"topK":4}`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"author":"assistant","content":"ok"}]}`)
	}))
	defer server.Close()
	request, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{
		BaseURL: server.URL + "/v1beta2/?tenant=example", Secret: "fixture key+&", UpstreamModel: "models/chat-bison-002",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	stream := (Provider{}).Decode(req, response)
	defer func() {
		if err := stream.Close(); err != nil {
			t.Error(err)
		}
	}()
	event, err := stream.Next()
	if err != nil || event.Kind != gateway.EventData || event.Usage != nil {
		t.Fatalf("event = %#v, err = %v", event, err)
	}
	if !bytes.Equal(req.Body, original) {
		t.Fatal("BuildRequest mutated the caller's body")
	}
}

func TestDefaultLegacyModelAndCancellationContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := (Provider{}).BuildRequest(ctx, chatRequest(), gateway.Target{Secret: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Host != "generativelanguage.googleapis.com" || req.URL.Path != "/v1beta2/models/chat-bison-001:generateMessage" {
		t.Errorf("unexpected native endpoint %s", req.URL.Path)
	}
	if !errors.Is(req.Context().Err(), context.Canceled) {
		t.Fatal("context cancellation was lost")
	}
}

func TestRejectUnsupportedOrInvalidInputs(t *testing.T) {
	cases := []string{
		`null`, `[]`, `{`, `{}`, `{"messages":[]}`,
		`{"messages":[{"role":"tool","content":"x"}]}`,
		`{"messages":[{"role":"user","content":""}]}`,
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://invalid.test/image"}}]}]}`,
		`{"messages":[{"role":"user","content":"x","name":"alice"}]}`,
		`{"messages":[{"role":"assistant","content":"x","tool_calls":[{}]}]}`,
		`{"messages":[{"role":"user","content":"x"},{"role":"system","content":"later"}]}`,
		`{"messages":[{"role":"system","content":"alone"}]}`,
		`{"messages":[{"role":"user","content":"x"}],"tools":[{}]}`,
		`{"messages":[{"role":"user","content":"x"}],"max_tokens":20}`,
		`{"messages":[{"role":"user","content":"x"}],"stop":["END"]}`,
		`{"messages":[{"role":"user","content":"x"}],"temperature":1.1}`,
		`{"messages":[{"role":"user","content":"x"}],"temperature":"0.5"}`,
		`{"messages":[{"role":"user","content":"x"}],"top_p":-1}`,
		`{"messages":[{"role":"user","content":"x"}],"top_k":1.2}`,
		`{"messages":[{"role":"user","content":"x"}],"top_k":0}`,
		`{"messages":[{"role":"user","content":"x"}],"n":9}`,
		`{"messages":[{"role":"user","content":"x"}],"stream_options":{"include_usage":"true"}}`,
		`{"messages":[{"role":"user","content":"x"}],"stream_options":{"other":true}}`,
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			req := chatRequest()
			req.Body = []byte(body)
			_, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "fixture"})
			var typed *gateway.UpstreamError
			if !errors.As(err, &typed) || typed.Status != http.StatusBadRequest {
				t.Fatalf("expected typed 400, got %v", err)
			}
		})
	}
}

func TestRejectBadTargetAndProtocol(t *testing.T) {
	for _, target := range []gateway.Target{
		{}, {Secret: "fixture", BaseURL: "ftp://invalid.test"}, {Secret: "fixture", BaseURL: "/relative"},
		{Secret: "fixture", BaseURL: "https://user:pass@invalid.test"},
		{Secret: "fixture", UpstreamModel: "../other?key=oops"},
	} {
		if _, err := (Provider{}).BuildRequest(context.Background(), chatRequest(), target); err == nil {
			t.Fatal("accepted invalid provider target")
		}
	}
	req := chatRequest()
	req.Protocol = gateway.ProtocolResponses
	if _, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "fixture"}); err == nil || !strings.Contains(err.Error(), "unsupported_protocol") {
		t.Fatal("accepted unsupported protocol")
	}
}

func chatRequest() *gateway.Request {
	return &gateway.Request{ID: "chatcmpl-fixture", Protocol: gateway.ProtocolOpenAIChat, Model: "PaLM-2",
		Body: []byte(`{"model":"PaLM-2","messages":[{"role":"user","content":"Hi"}]}`)}
}

func assertJSONFixture(t *testing.T, actual []byte, fixture string) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal(actual, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(fixture), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON mismatch:\nactual %s\nwanted %s", actual, fixture)
	}
}
