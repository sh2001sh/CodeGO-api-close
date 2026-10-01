package routing

import (
	"sort"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// OfficialAttemptMetrics contains observed upstream values only. Full request
// duration is not a substitute for time to the first response event.
type OfficialAttemptMetrics struct {
	TTFT         time.Duration
	PromptTokens int64
	CachedTokens int64
}

type officialHealthKey struct {
	channel int64
	model   string
}

type officialWindow struct {
	start               time.Time
	requests, successes int64
}

type officialTTFT struct {
	at    time.Time
	value float64
}

type officialStats struct {
	w5, w15      officialWindow
	last         time.Time
	ttft         []officialTTFT
	cacheRate    float64
	cacheSamples int
	failures     int
}

type officialHealth struct {
	mu        sync.Mutex
	values    map[officialHealthKey]officialStats
	lastSweep time.Time
}

func newOfficialHealth() *officialHealth {
	return &officialHealth{values: map[officialHealthKey]officialStats{}}
}

func (h *officialHealth) observe(target gateway.Target, result gateway.AttemptResult, metrics OfficialAttemptMetrics, now time.Time) {
	if h == nil || target.ChannelID <= 0 || target.UpstreamModel == "" || result.Scope == gateway.ScopeRequest || (!result.OK && !result.Retryable) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if now.Sub(h.lastSweep) >= time.Minute {
		for key, state := range h.values {
			if now.Sub(state.last) >= 15*time.Minute {
				delete(h.values, key)
			}
		}
		h.lastSweep = now
	}
	key := officialHealthKey{target.ChannelID, target.UpstreamModel}
	state := h.values[key]
	state.last = now
	for _, window := range []struct {
		value *officialWindow
		ttl   time.Duration
	}{{&state.w5, 5 * time.Minute}, {&state.w15, 15 * time.Minute}} {
		if window.value.start.IsZero() || now.Sub(window.value.start) >= window.ttl {
			*window.value = officialWindow{start: now}
			if window.ttl == 5*time.Minute {
				state.cacheRate, state.cacheSamples = 0, 0
			}
		}
		window.value.requests++
		if result.OK {
			window.value.successes++
		}
	}
	if result.OK {
		state.failures = 0
		if metrics.TTFT > 0 {
			state.ttft = append(state.ttft, officialTTFT{now, float64(metrics.TTFT) / float64(time.Millisecond)})
			if len(state.ttft) > 20 {
				state.ttft = state.ttft[len(state.ttft)-20:]
			}
		}
		if metrics.PromptTokens > 0 && metrics.CachedTokens >= 0 {
			rate := float64(min(metrics.CachedTokens, metrics.PromptTokens)) / float64(metrics.PromptTokens) * 100
			if state.cacheSamples == 0 {
				state.cacheRate = rate
			} else {
				state.cacheRate = state.cacheRate*0.8 + rate*0.2
			}
			state.cacheSamples++
		}
	} else {
		state.failures++
	}
	h.values[key] = state
}

func (h *officialHealth) read(channel int64, model string, now time.Time) officialStats {
	if h == nil {
		return officialStats{}
	}
	h.mu.Lock()
	state := h.values[officialHealthKey{channel, model}]
	state.ttft = append([]officialTTFT(nil), state.ttft...)
	h.mu.Unlock()
	if now.Sub(state.last) >= 15*time.Minute {
		return officialStats{}
	}
	if now.Sub(state.w5.start) >= 5*time.Minute {
		state.w5, state.cacheRate, state.cacheSamples = officialWindow{}, 0, 0
	}
	if now.Sub(state.w15.start) >= 15*time.Minute {
		state.w15 = officialWindow{}
	}
	return state
}

func (s officialStats) p95(now time.Time) float64 {
	values := make([]float64, 0, len(s.ttft))
	for _, sample := range s.ttft {
		if now.Sub(sample.at) < 15*time.Minute {
			values = append(values, sample.value)
		}
	}
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	return values[(len(values)*95+99)/100-1]
}
