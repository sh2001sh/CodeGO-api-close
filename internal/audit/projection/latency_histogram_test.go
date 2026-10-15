package projection

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHistogramPercentilesAndOverflow(t *testing.T) {
	values := make([]int64, metricHistogramBuckets)
	for _, latency := range []int64{80, 200, 800, 4000, 16000, 310000} {
		values[metricHistogramIndex(latency)]++
	}

	require.EqualValues(t, 1000, histogramPercentile(values, 0.5))
	require.EqualValues(t, 300000, histogramPercentile(values, 0.95))
	require.Equal(t, metricHistogramBuckets-1, metricHistogramIndex(310000))
}

func TestHistogramPercentileRequiresNewSamples(t *testing.T) {
	require.Zero(t, histogramPercentile(nil, 0.5))
	require.Zero(t, histogramPercentile(make([]int64, metricHistogramBuckets), 0.95))
}
