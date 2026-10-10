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
	info.ResponseCompletedAt = time.Time{}
}

// AttemptTTFT reports latency attributable to the final upstream attempt.
func (info *RelayInfo) AttemptTTFT() (time.Duration, bool) {
	if info == nil || info.AttemptStartTime.IsZero() || !info.HasSemanticResponse() {
		return 0, false
	}
	value := info.FirstResponseTime.Sub(info.AttemptStartTime)
	return validRelayDuration(value)
}

// EndToEndTTFT reports request latency after the client upload has completed.
// This keeps the metric comparable with upstream provider TTFT while retaining
// local validation, routing, billing and retry time after body reception.
func (info *RelayInfo) EndToEndTTFT() (time.Duration, bool) {
	if info == nil || info.StartTime.IsZero() || !info.HasSemanticResponse() {
		return 0, false
	}
	start := info.StartTime
	if info.FirstByteTrace != nil {
		if bodyDone := info.FirstByteTrace.BodyReadDoneTime(); !bodyDone.IsZero() && bodyDone.After(start) {
			start = bodyDone
		}
	}
	value := info.FirstResponseTime.Sub(start)
	return validRelayDuration(value)
}

func validRelayDuration(value time.Duration) (time.Duration, bool) {
	if value < 0 {
		return 0, false
	}
	return value, true
}

// MarkResponseCompleted freezes the response interval before billing and audit
// work. Call it after the response handler has finished, including final frames.
func (info *RelayInfo) MarkResponseCompleted() {
	info.MarkResponseCompletedAt(time.Now())
}

func (info *RelayInfo) MarkResponseCompletedAt(completedAt time.Time) {
	if info == nil || completedAt.IsZero() || !info.ResponseCompletedAt.IsZero() {
		return
	}
	info.ResponseCompletedAt = completedAt
}

func (info *RelayInfo) ResponseDuration() (time.Duration, bool) {
	if info == nil || info.StartTime.IsZero() || info.ResponseCompletedAt.IsZero() {
		return 0, false
	}
	return validRelayDuration(info.ResponseCompletedAt.Sub(info.StartTime))
}

// GenerationDuration is observed streaming output time, including reasoning
// and tool output. It does not describe the provider's internal generation rate.
// Missing observations and non-streaming responses deliberately remain unknown.
func (info *RelayInfo) GenerationDuration() (time.Duration, bool) {
	if info == nil || !info.IsStream || !info.HasSemanticResponse() || info.ResponseCompletedAt.IsZero() {
		return 0, false
	}
	value := info.ResponseCompletedAt.Sub(info.FirstResponseTime)
	return value, value > 0
}

// ObserveStreamOutput records model output after decoding, separately from raw
// SSE receipt. Text excludes reasoning, tool arguments and lifecycle events.
func (info *RelayInfo) ObserveStreamOutput(receivedAt time.Time, isText bool) {
	if info == nil {
		return
	}
	if info.FirstByteTrace != nil {
		info.FirstByteTrace.MarkFirstSemanticReadAt(receivedAt, isText)
		if isText {
			info.FirstByteTrace.MarkFirstTextReadAt(receivedAt)
			info.FirstByteTrace.MarkFirstTextEvent()
		}
	}
	info.SetFirstSemanticResponseTime()
}
