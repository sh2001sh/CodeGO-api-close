package runtime

import "time"

// BeginAttempt resets the channel-local latency origin immediately before an
// upstream attempt starts. Request-scoped StartTime intentionally stays fixed.
func (info *RelayInfo) BeginAttempt(startedAt time.Time) {
	if info == nil {
		return
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	info.AttemptStartTime = startedAt
}

// AttemptTTFT reports latency attributable to the final upstream attempt.
func (info *RelayInfo) AttemptTTFT() (time.Duration, bool) {
	if info == nil || info.AttemptStartTime.IsZero() || !info.HasSendResponse() {
		return 0, false
	}
	value := info.FirstResponseTime.Sub(info.AttemptStartTime)
	return validRelayDuration(value)
}

// EndToEndTTFT reports request latency including local work, routing and retry.
func (info *RelayInfo) EndToEndTTFT() (time.Duration, bool) {
	if info == nil || info.StartTime.IsZero() || !info.HasSendResponse() {
		return 0, false
	}
	value := info.FirstResponseTime.Sub(info.StartTime)
	return validRelayDuration(value)
}

func validRelayDuration(value time.Duration) (time.Duration, bool) {
	if value < 0 {
		return 0, false
	}
	return value, true
}
