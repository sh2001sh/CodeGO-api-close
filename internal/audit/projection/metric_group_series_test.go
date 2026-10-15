package projection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMergeHotGroupModelBucketsDoesNotDoubleCountRedisBucket(t *testing.T) {
	t.Parallel()

	bucketTs := bucketStart(time.Now().Unix())
	key := bucketKey{model: "redis-model", group: "redis-group", bucketTs: bucketTs}
	hotBuckets.Store(key, &atomicBucket{})
	hot, _ := hotBuckets.Load(key)
	hot.(*atomicBucket).requestCount.Store(5)
	hot.(*atomicBucket).successCount.Store(5)
	t.Cleanup(func() { hotBuckets.Delete(key) })

	buckets := map[modelGroupKey]map[int64]counters{
		{group: key.group, model: key.model}: {
			bucketTs: {requestCount: 5, successCount: 5},
		},
	}
	mergeHotGroupModelBuckets(
		buckets,
		map[string]struct{}{key.group: {}},
		bucketTs,
		bucketTs+getPerfMetricsBucketSeconds(),
		map[bucketKey]struct{}{key: {}},
	)

	value := buckets[modelGroupKey{group: key.group, model: key.model}][bucketTs]
	require.EqualValues(t, 5, value.requestCount)
	require.EqualValues(t, 5, value.successCount)
}
