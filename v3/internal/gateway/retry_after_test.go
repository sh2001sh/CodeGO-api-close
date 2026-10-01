package gateway

import (
	"net/http"
	"testing"
	"time"
)

func TestProviderRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		headers map[string]string
		body    string
		want    time.Duration
	}{
		{"explicit wins", map[string]string{"Retry-After": "5", "x-codex-primary-reset-after-seconds": "7200"}, "", 5 * time.Second},
		{"anthropic exhausted week", map[string]string{"anthropic-ratelimit-unified-7d-status": "rejected", "anthropic-ratelimit-unified-7d-reset": "2026-10-01T00:00:00Z"}, "", 24 * time.Hour},
		{"anthropic minute excludes healthy week", map[string]string{"anthropic-ratelimit-requests-remaining": "0", "anthropic-ratelimit-requests-reset": "2026-09-30T00:00:05Z", "anthropic-ratelimit-unified-7d-status": "allowed", "anthropic-ratelimit-unified-7d-reset": "2026-10-01T00:00:00Z"}, "", 5 * time.Second},
		{"codex only exhausted window", map[string]string{"x-codex-primary-used-percent": "100", "x-codex-primary-reset-after-seconds": "60", "x-codex-secondary-used-percent": "50", "x-codex-secondary-reset-after-seconds": "86400"}, "", time.Minute},
		{"gemini retry info", nil, `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"12.5s"}]}}`, 12500 * time.Millisecond},
		{"expired and malformed", map[string]string{"Retry-After": "invalid", "anthropic-ratelimit-unified-reset": "2026-09-29T00:00:00Z", "x-codex-primary-reset-after-seconds": "-1"}, `{}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := make(http.Header)
			for key, value := range tc.headers {
				h.Set(key, value)
			}
			if got := providerRetryAfter(h, []byte(tc.body), now); got != tc.want {
				t.Fatalf("delay=%s want=%s", got, tc.want)
			}
		})
	}
}
