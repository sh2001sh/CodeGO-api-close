package runtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRelayLatencySeparatesAttemptAndEndToEndTTFT(t *testing.T) {
	requestStart := time.Now().Add(-12 * time.Second)
	attemptStart := requestStart.Add(8 * time.Second)
	info := &RelayInfo{
		StartTime:             requestStart,
		AttemptStartTime:      attemptStart,
		FirstResponseTime:     attemptStart.Add(3 * time.Second),
		firstSemanticResponse: true,
	}

	attempt, ok := info.AttemptTTFT()
	require.True(t, ok)
	require.Equal(t, 3*time.Second, attempt)
	e2e, ok := info.EndToEndTTFT()
	require.True(t, ok)
	require.Equal(t, 11*time.Second, e2e)
}

func TestEndToEndTTFTExcludesClientUpload(t *testing.T) {
	requestStart := time.Now().Add(-12 * time.Second)
	bodyDone := requestStart.Add(4 * time.Second)
	responseAt := requestStart.Add(11 * time.Second)
	trace := NewFirstByteTrace(requestStart)
	trace.bodyReadDoneAt = bodyDone
	info := &RelayInfo{
		StartTime:             requestStart,
		FirstResponseTime:     responseAt,
		FirstByteTrace:        trace,
		firstSemanticResponse: true,
	}

	ttft, ok := info.EndToEndTTFT()
	require.True(t, ok)
	require.Equal(t, 7*time.Second, ttft)
}

func TestResponseTimingFreezesBeforeBillingAndAuditDelay(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	info := &RelayInfo{StartTime: start, IsStream: true, FirstResponseTime: start.Add(2 * time.Second), firstSemanticResponse: true}
	info.MarkResponseCompletedAt(start.Add(5 * time.Second))
	info.MarkResponseCompletedAt(time.Now()) // Later settlement/audit must not replace it.
	duration, ok := info.ResponseDuration()
	require.True(t, ok)
	require.Equal(t, 5*time.Second, duration)
	generation, ok := info.GenerationDuration()
	require.True(t, ok)
	require.Equal(t, 3*time.Second, generation)
}

func TestGenerationTimingRejectsUnobservedAndNonStreamingOutput(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	for _, test := range []struct {
		name             string
		stream, semantic bool
		end              time.Time
	}{
		{name: "non-stream", semantic: true, end: start.Add(10 * time.Second)},
		{name: "lifecycle only", stream: true, end: start.Add(10 * time.Second)},
		{name: "missing completion", stream: true, semantic: true},
		{name: "zero interval", stream: true, semantic: true, end: start.Add(time.Second)},
		{name: "inverted interval", stream: true, semantic: true, end: start},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &RelayInfo{StartTime: start, FirstResponseTime: start.Add(time.Second), IsStream: test.stream, firstSemanticResponse: test.semantic}
			info.MarkResponseCompletedAt(test.end)
			_, ok := info.GenerationDuration()
			require.False(t, ok)
		})
	}
}

func TestBeginAttemptClearsPriorCompletion(t *testing.T) {
	info := &RelayInfo{ResponseCompletedAt: time.Now()}
	info.BeginAttempt(time.Now())
	require.True(t, info.ResponseCompletedAt.IsZero())
}

func TestRelayLatencyRejectsMissingResponse(t *testing.T) {
	startedAt := time.Now()
	info := &RelayInfo{
		StartTime:         startedAt,
		AttemptStartTime:  startedAt,
		FirstResponseTime: startedAt.Add(-time.Second),
	}

	_, attemptOK := info.AttemptTTFT()
	_, e2eOK := info.EndToEndTTFT()
	require.False(t, attemptOK)
	require.False(t, e2eOK)
}
