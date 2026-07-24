package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStreamPacer_LeavesFirstTextDeltaImmediate(t *testing.T) {
	pacer := NewStreamPacer("gpt-5.6-sol")
	started := time.Now()
	require.NoError(t, pacer.Pace(context.Background(), "hello"))
	require.Less(t, time.Since(started), 20*time.Millisecond)
}

func TestStreamPacer_DelaysFollowingGPTText(t *testing.T) {
	pacer := NewStreamPacer("gpt-5.6-sol")
	require.NoError(t, pacer.Pace(context.Background(), "hello"))
	started := time.Now()
	require.NoError(t, pacer.Pace(context.Background(), "one two three four five six seven eight nine ten"))
	require.GreaterOrEqual(t, time.Since(started), 100*time.Millisecond)
}

func TestStreamPacer_UsesCumulativeGenerationSchedule(t *testing.T) {
	pacer := NewStreamPacer("gpt-5.6-sol")
	require.NoError(t, pacer.Pace(context.Background(), "hello world"))

	started := time.Now()
	require.NoError(t, pacer.Pace(context.Background(), "one two three four five six seven eight nine ten"))
	require.GreaterOrEqual(t, time.Since(started), 100*time.Millisecond)
}

func TestStreamPacer_StopsWhenRequestIsCancelled(t *testing.T) {
	pacer := NewStreamPacer("gpt-5.6-sol")
	require.NoError(t, pacer.Pace(context.Background(), "hello"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, pacer.Pace(ctx, "one two three four five six seven eight nine ten"), context.Canceled)
}

func TestStreamPacer_SplitsLargeFirstDeltaWithoutChangingText(t *testing.T) {
	pacer := NewStreamPacer("gpt-5.6-sol")
	text := "one two three four five six seven eight nine ten"
	parts := pacer.SplitText(text)

	require.Greater(t, len(parts), 1)
	require.Equal(t, text, strings.Join(parts, ""))
	require.LessOrEqual(t, estimateStreamTokens(parts[0]), firstStreamChunkTokenBudget)
}

func TestStreamPacer_TracksReleasedVisibleTokens(t *testing.T) {
	pacer := NewStreamPacer("gpt-5.6-sol")
	text := "one two three four five six seven eight nine ten"
	parts := pacer.SplitText(text)
	expectedTokens := 0
	for _, part := range parts {
		require.NoError(t, pacer.Pace(context.Background(), part))
		expectedTokens += estimateStreamTokens(part)
	}

	require.Equal(t, expectedTokens, pacer.OutputTokens())
	duration, measured := pacer.OutputDuration()
	require.True(t, measured)
	require.Positive(t, duration)
}

func TestStreamPacer_SkipsNonGPTModels(t *testing.T) {
	require.Nil(t, NewStreamPacer("claude-sonnet-4"))
}
