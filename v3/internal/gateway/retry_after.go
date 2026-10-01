package gateway

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// providerRetryAfter prefers an explicit Retry-After and only considers
// exhausted long windows, so a minute limit cannot cool a usable weekly pool.
func providerRetryAfter(header http.Header, body []byte, now time.Time) time.Duration {
	if d := parseRetryAfter(header.Get("Retry-After"), now); d > 0 {
		return d
	}
	var delay time.Duration
	reset := func(key string) { delay = max(delay, resetDelay(header.Get(key), now)) }
	reset("anthropic-ratelimit-unified-reset")
	for _, window := range []string{"5h", "7d"} {
		prefix := "anthropic-ratelimit-unified-" + window
		status := header.Get(prefix + "-status")
		utilization, _ := strconv.ParseFloat(header.Get(prefix+"-utilization"), 64)
		if status == "rejected" || status == "blocked" || utilization >= 1 {
			reset(prefix + "-reset")
		}
	}
	if delay == 0 {
		for _, scope := range []string{"requests", "tokens", "input-tokens", "output-tokens"} {
			prefix := "anthropic-ratelimit-" + scope
			if header.Get(prefix+"-remaining") == "0" {
				reset(prefix + "-reset")
			}
		}
	}
	for _, window := range []string{"primary", "secondary"} {
		prefix := "x-codex-" + window
		used := header.Get(prefix + "-used-percent")
		percent, _ := strconv.ParseFloat(used, 64)
		if used != "" && percent < 100 {
			continue
		}
		if seconds, err := strconv.ParseInt(header.Get(prefix+"-reset-after-seconds"), 10, 64); err == nil && seconds > 0 && seconds <= int64((365*24*time.Hour)/time.Second) {
			delay = max(delay, time.Duration(seconds)*time.Second)
		}
		reset(prefix + "-reset-at")
	}
	for _, detail := range gjson.GetBytes(body, "error.details").Array() {
		if !strings.HasSuffix(detail.Get("@type").Str, "google.rpc.RetryInfo") {
			continue
		}
		value := detail.Get("retryDelay")
		if d, err := time.ParseDuration(value.Str); err == nil && d > 0 {
			delay = max(delay, d)
		}
		if seconds := value.Get("seconds").Int(); seconds > 0 && seconds <= int64((365*24*time.Hour)/time.Second) {
			delay = max(delay, time.Duration(seconds)*time.Second+time.Duration(value.Get("nanos").Int()))
		}
	}
	if gjson.GetBytes(body, "error.type").Str == "usage_limit_reached" {
		delay = max(delay, resetDelay(gjson.GetBytes(body, "error.resets_at").String(), now))
		if seconds := gjson.GetBytes(body, "error.retry_after_seconds").Int(); seconds > 0 && seconds <= int64((365*24*time.Hour)/time.Second) {
			delay = max(delay, time.Duration(seconds)*time.Second)
		}
	}
	return delay
}

func resetDelay(value string, now time.Time) time.Duration {
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil && unix > now.Unix() {
		return time.Unix(unix, 0).Sub(now)
	}
	if reset, err := time.Parse(time.RFC3339, value); err == nil && reset.After(now) {
		return reset.Sub(now)
	}
	return 0
}
