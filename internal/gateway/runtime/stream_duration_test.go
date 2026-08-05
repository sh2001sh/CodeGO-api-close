package runtime

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestStreamMaxDurationUsesGPTTiers(t *testing.T) {
	oldNormal := constant.StreamingMaxDuration
	oldLong := constant.StreamingLongContextMaxDuration
	t.Cleanup(func() {
		constant.StreamingMaxDuration = oldNormal
		constant.StreamingLongContextMaxDuration = oldLong
	})
	constant.StreamingMaxDuration = 240
	constant.StreamingLongContextMaxDuration = 540

	require.Equal(t, 240*time.Second, StreamMaxDuration("gpt-5.6-sol", 10_000))
	require.Equal(t, 540*time.Second, StreamMaxDuration("gpt-5.6-sol", LongContextPromptTokenThreshold))
	require.Zero(t, StreamMaxDuration("claude-opus", 10_000))
}
