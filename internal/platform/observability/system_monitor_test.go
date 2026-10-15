package observability

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNextCPUHighSampleCountRequiresSustainedLoad(t *testing.T) {
	count := nextCPUHighSampleCount(0, 96.4, 90)
	require.Equal(t, 1, count)
	count = nextCPUHighSampleCount(count, 92.0, 90)
	require.Equal(t, 2, count)
	count = nextCPUHighSampleCount(count, 91.0, 90)
	require.Equal(t, CPUOverloadSampleThreshold, count)
	require.Equal(t, CPUOverloadSampleThreshold, nextCPUHighSampleCount(count, 99.0, 90))
}

func TestNextCPUHighSampleCountResetsAfterNormalSample(t *testing.T) {
	require.Zero(t, nextCPUHighSampleCount(2, 89.9, 90))
	require.Zero(t, nextCPUHighSampleCount(2, 100, 0))
}
