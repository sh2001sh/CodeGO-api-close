package runtime

import (
	"sort"
	"time"
)

func recordChannelTTFT(state *ChannelHealth, value float64) {
	if state == nil || value <= 0 {
		return
	}
	state.TTFTSamples++
	if state.TTFTEWMAMilliseconds == 0 {
		state.TTFTEWMAMilliseconds = value
	} else {
		state.TTFTEWMAMilliseconds = state.TTFTEWMAMilliseconds*0.8 + value*0.2
	}
	state.TTFTRecentMilliseconds = append(state.TTFTRecentMilliseconds, int64(value))
	if len(state.TTFTRecentMilliseconds) > channelHealthTTFTWindow {
		state.TTFTRecentMilliseconds = state.TTFTRecentMilliseconds[len(state.TTFTRecentMilliseconds)-channelHealthTTFTWindow:]
	}
	samples := append([]int64(nil), state.TTFTRecentMilliseconds...)
	sort.Slice(samples, func(i int, j int) bool { return samples[i] < samples[j] })
	state.TTFTP50Milliseconds = percentile(samples, 50)
	state.TTFTP95Milliseconds = percentile(samples, 95)
}

func percentile(samples []int64, percentage int) float64 {
	if len(samples) == 0 {
		return 0
	}
	index := (len(samples)*percentage + 99) / 100
	if index > 0 {
		index--
	}
	return float64(samples[index])
}

func recordChannelHealthWindow(state *ChannelHealth, now time.Time, success bool) {
	if state.Window2StartedAt.IsZero() || now.Sub(state.Window2StartedAt) >= channelHealthShortWindow {
		state.Window2StartedAt = now
		state.Window2Requests = 0
		state.Window2Successes = 0
	}
	if state.Window5StartedAt.IsZero() || now.Sub(state.Window5StartedAt) >= 5*time.Minute {
		state.Window5StartedAt = now
		state.Window5Requests = 0
		state.Window5Successes = 0
	}
	if state.Window15StartedAt.IsZero() || now.Sub(state.Window15StartedAt) >= 15*time.Minute {
		state.Window15StartedAt = now
		state.Window15Requests = 0
		state.Window15Successes = 0
	}
	state.Window2Requests++
	state.Window5Requests++
	state.Window15Requests++
	if success {
		state.Window2Successes++
		state.Window5Successes++
		state.Window15Successes++
	}
	state.SuccessRate2m = float64(state.Window2Successes) / float64(state.Window2Requests) * 100
	state.SuccessRate5m = float64(state.Window5Successes) / float64(state.Window5Requests) * 100
	state.SuccessRate15m = float64(state.Window15Successes) / float64(state.Window15Requests) * 100
}
