package live

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

func TestTrackingProviderRemembersSelectedCredentialBeforeOutput(t *testing.T) {
	repo := &liveRepo{items: map[string]Locator{}}
	h := &Handler{cfg: Config{Repository: repo, LocatorTTL: time.Hour, FinalizeTimeout: time.Second}}
	provider := h.TrackingProvider(responses.Provider{})
	req := &gateway.Request{ID: "request", Principal: gateway.Principal{UserID: 1, KeyID: 2}, Protocol: gateway.ProtocolResponses, Model: "model", Stream: true, Body: []byte(`{"model":"model","input":"hello","stream":true}`)}
	target := gateway.Target{ChannelID: 3, CredentialID: 4, Provider: "openai", Secret: "must-not-be-stored", BaseURL: "https://upstream.invalid"}
	up, err := provider.BuildRequest(context.Background(), req, target)
	if err != nil {
		t.Fatal(err)
	}
	body := `data: {"type":"response.output_text.delta","delta":"hello","response":{"id":"resp_native"}}` + "\n\n" + `data: {"type":"response.completed","response":{"id":"resp_native","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n"
	resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: up}
	resp.Header.Set("Content-Type", "text/event-stream")
	stream := provider.Decode(req, resp)
	defer func() { _ = stream.Close() }()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	item, err := repo.Get(context.Background(), "resp_native", 1, 2)
	if err != nil || item.ChannelID != 3 || item.CredentialID != 4 {
		t.Fatalf("locator=%+v %v", item, err)
	}
	repo.err = errors.New("Redis unavailable")
	// A distinct ID is denied instead of exposed without a durable owner locator.
	resp.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(body, "resp_native", "resp_missing")))
	stream = provider.Decode(req, resp)
	defer func() { _ = stream.Close() }()
	event, err := stream.Next()
	if err != nil || event.Kind != gateway.EventError || event.Err.Code != "response_store_unavailable" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}
