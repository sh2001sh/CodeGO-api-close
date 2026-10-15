package projection

import (
	"math"
	"sync/atomic"
)

// The extra bucket stores values above the final upper bound. Percentiles in
// that overflow bucket are reported as the 300-second display cap.
var metricHistogramBoundsMs = [...]int64{
	100, 250, 500, 1000, 2000, 3000, 5000, 7500, 10000,
	15000, 20000, 30000, 45000, 60000, 90000, 120000, 180000, 300000,
}

const metricHistogramBuckets = len(metricHistogramBoundsMs) + 1

func metricHistogramIndex(value int64) int {
	for index, bound := range metricHistogramBoundsMs {
		if value <= bound {
			return index
		}
	}
	return metricHistogramBuckets - 1
}

func atomicHistogramSnapshot(values *[metricHistogramBuckets]atomic.Int64) []int64 {
	result := make([]int64, metricHistogramBuckets)
	for index := range values {
		result[index] = values[index].Load()
	}
	return result
}

func atomicHistogramDrain(values *[metricHistogramBuckets]atomic.Int64) []int64 {
	result := make([]int64, metricHistogramBuckets)
	for index := range values {
		result[index] = values[index].Swap(0)
	}
	return result
}

func atomicHistogramAdd(target *[metricHistogramBuckets]atomic.Int64, values []int64) {
	for index, value := range values {
		if index >= metricHistogramBuckets {
			break
		}
		if value != 0 {
			target[index].Add(value)
		}
	}
}

func histogramPercentile(values []int64, percentile float64) int64 {
	if len(values) != metricHistogramBuckets {
		return 0
	}
	var total int64
	for _, value := range values {
		total += value
	}
	if total <= 0 {
		return 0
	}
	target := max(int64(math.Ceil(float64(total)*percentile)), 1)
	var cumulative int64
	for index, value := range values {
		cumulative += value
		if cumulative < target {
			continue
		}
		if index >= len(metricHistogramBoundsMs) {
			return metricHistogramBoundsMs[len(metricHistogramBoundsMs)-1]
		}
		return metricHistogramBoundsMs[index]
	}
	return metricHistogramBoundsMs[len(metricHistogramBoundsMs)-1]
}

func mergeHistogram(target, value []int64) []int64 {
	if len(target) != metricHistogramBuckets {
		target = make([]int64, metricHistogramBuckets)
	}
	for index, count := range value {
		if index >= metricHistogramBuckets {
			break
		}
		target[index] += count
	}
	return target
}
