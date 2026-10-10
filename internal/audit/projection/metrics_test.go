package projection

import (
	"testing"
	"time"

	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/stretchr/testify/require"
)

func TestRecordRelayUsageSampleExcludesBillingDelayAndUnknownGeneration(t *testing.T) {
	originalRedis := platformcache.RedisEnabled
	platformcache.RedisEnabled = false
	t.Cleanup(func() { platformcache.RedisEnabled = originalRedis })
	start := time.Now().Add(-time.Hour)
	for _, test := range []struct {
		name                       string
		stream, semantic, complete bool
		generationMs, tokens       int64
	}{
		{name: "stream", stream: true, semantic: true, complete: true, generationMs: 3000, tokens: 60},
		{name: "non-stream", semantic: true, complete: true},
		{name: "lifecycle only", stream: true, complete: true},
		{name: "no completion", stream: true, semantic: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: t.Name(), UsingGroup: "default", StartTime: start, IsStream: test.stream, ChannelMeta: &relaycommon.ChannelMeta{}}
			if test.semantic {
				info.SetFirstSemanticResponseTime()
			}
			info.FirstResponseTime = start.Add(2 * time.Second)
			if test.complete {
				info.MarkResponseCompletedAt(start.Add(5 * time.Second))
			}
			key := bucketKey{model: info.OriginModelName, group: info.UsingGroup, bucketTs: bucketStart(time.Now().Unix())}
			t.Cleanup(func() { hotBuckets.Delete(key) })
			RecordRelayUsageSample(info, true, 100, 20, 10, 60)
			stored, ok := hotBuckets.Load(key)
			require.True(t, ok)
			sample := stored.(*atomicBucket).snapshot()
			require.Equal(t, test.generationMs, sample.generationMs)
			require.Equal(t, test.tokens, sample.outputTokens)
			require.EqualValues(t, 100, sample.inputTokens)
			require.EqualValues(t, 20, sample.cacheReadTokens)
			require.EqualValues(t, 10, sample.cacheWriteTokens)
			if test.complete {
				require.EqualValues(t, 5000, sample.totalLatencyMs)
			} else {
				require.GreaterOrEqual(t, sample.totalLatencyMs, int64(time.Hour/time.Millisecond), "unknown completion must not fabricate zero latency")
			}
			if test.stream && test.semantic && test.complete {
				require.Equal(t, 20.0, avgTps(sample))
			} else {
				require.Zero(t, avgTps(sample))
			}
		})
	}
}
