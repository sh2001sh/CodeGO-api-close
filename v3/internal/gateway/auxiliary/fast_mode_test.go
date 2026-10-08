package auxiliary

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestCompactResponseTierWithActualOrEstimatedUsage(t *testing.T) {
	for _, body := range []string{`{"service_tier":"default","usage":{"input_tokens":5,"output_tokens":2}}`, `{"response":{"service_tier":"default","usage":{"input_tokens":5,"output_tokens":2}}}`} {
		u := parseUsage([]byte(body))
		if u == nil || u.ServiceTier != "default" || u.PromptTokens != 5 {
			t.Fatalf("usage=%+v", u)
		}
	}
	u := estimate(&gateway.Request{Body: []byte(`{"input":"hello"}`)}, Input{Operation: Compact}, []byte(`{"service_tier":"default","output":[]}`))
	if !u.Estimated || u.ServiceTier != "default" {
		t.Fatalf("estimate=%+v", u)
	}
	r := &sseRelay{usage: gateway.Usage{Estimated: true}}
	for _, body := range []string{`{"response":{"service_tier":"fast"}}`, `{"response":{"usage":{"input_tokens":5,"output_tokens":2}}}`, `{"response":{"service_tier":"default"}}`} {
		if r.classifyPayload(body, gjson.Parse(body)) {
			t.Fatal("unexpected stream error")
		}
	}
	if r.usage.ServiceTier != "default" || r.usage.PromptTokens != 5 {
		t.Fatalf("latest tier=%+v", r.usage)
	}
}
