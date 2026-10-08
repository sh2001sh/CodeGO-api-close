package openai_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

func TestChatActualTierPersistsAcrossUsageAndErrors(t *testing.T) {
	for _, tail := range []string{
		`{"service_tier":"default","usage":{"prompt_tokens":5,"completion_tokens":2},"choices":[]}`,
		`{"service_tier":"default","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"service_tier":"default","error":{"message":"upstream interrupted"}}`,
	} {
		s := (openai.Provider{}).Decode(&gateway.Request{Stream: true, Body: []byte(`{}`)}, &http.Response{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"service_tier\":\"fast\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: " + tail + "\n\ndata: [DONE]\n\n"))})
		var tier string
		for {
			ev, err := s.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if ev.ServiceTier != "" {
				tier = ev.ServiceTier
			}
			if ev.Kind == gateway.EventDone || ev.Kind == gateway.EventError {
				break
			}
		}
		_ = s.Close()
		if tier != "default" {
			t.Fatalf("tail lost latest tier: %s tier=%q", tail, tier)
		}
	}
}
