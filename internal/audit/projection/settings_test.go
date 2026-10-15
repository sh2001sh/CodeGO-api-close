package projection

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPerfMetricsBucketSecondsUsesConfiguredWidth(t *testing.T) {
	original := projectionPerfMetricsSetting
	t.Cleanup(func() { projectionPerfMetricsSetting = original })

	projectionPerfMetricsSetting.BucketTime = "minute"
	require.EqualValues(t, 60, PerfMetricsBucketSeconds())

	projectionPerfMetricsSetting.BucketTime = "5min"
	require.EqualValues(t, 300, PerfMetricsBucketSeconds())

	projectionPerfMetricsSetting.BucketTime = "hour"
	require.EqualValues(t, 3600, PerfMetricsBucketSeconds())
}
