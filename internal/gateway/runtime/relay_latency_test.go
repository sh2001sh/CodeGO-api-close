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
		StartTime:         requestStart,
		AttemptStartTime:  attemptStart,
		FirstResponseTime: attemptStart.Add(3 * time.Second),
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
		StartTime:         requestStart,
		FirstResponseTime: responseAt,
		FirstByteTrace:    trace,
	}

	ttft, ok := info.EndToEndTTFT()
	require.True(t, ok)
	require.Equal(t, 7*time.Second, ttft)
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
