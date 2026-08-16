package projection

import "sync/atomic"

type Store interface {
	Record(sample Sample)
	Query(params QueryParams) (QueryResult, error)
}

type Sample struct {
	Model            string
	Group            string
	ChannelID        int
	LatencyMs        int64
	TtftMs           int64
	HasTtft          bool
	Success          bool
	OutputTokens     int64
	GenerationMs     int64
	InputTokens      int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

type QueryParams struct {
	Model string
	Group string
	Hours int
}

type BucketPoint struct {
	Ts               int64   `json:"ts"`
	AvgTtftMs        int64   `json:"avg_ttft_ms"`
	AvgLatencyMs     int64   `json:"avg_latency_ms"`
	SuccessRate      float64 `json:"success_rate"`
	AvgTps           float64 `json:"avg_tps"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	InputTokens      int64   `json:"input_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	RequestCount     int64   `json:"request_count"`
}

type GroupResult struct {
	Group            string        `json:"group"`
	AvgTtftMs        int64         `json:"avg_ttft_ms"`
	AvgLatencyMs     int64         `json:"avg_latency_ms"`
	SuccessRate      float64       `json:"success_rate"`
	AvgTps           float64       `json:"avg_tps"`
	CacheHitRate     float64       `json:"cache_hit_rate"`
	InputTokens      int64         `json:"input_tokens"`
	CacheReadTokens  int64         `json:"cache_read_tokens"`
	CacheWriteTokens int64         `json:"cache_write_tokens"`
	Series           []BucketPoint `json:"series"`
}

type QueryResult struct {
	ModelName    string        `json:"model_name"`
	SeriesSchema string        `json:"series_schema"`
	Groups       []GroupResult `json:"groups"`
}

type ModelSummary struct {
	ModelName        string  `json:"model_name"`
	AvgTtftMs        int64   `json:"avg_ttft_ms"`
	AvgLatencyMs     int64   `json:"avg_latency_ms"`
	SuccessRate      float64 `json:"success_rate"`
	AvgTps           float64 `json:"avg_tps"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	InputTokens      int64   `json:"input_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	RequestCount     int64   `json:"request_count"`
}

type SummaryAllResult struct {
	Models []ModelSummary `json:"models"`
}

type GroupSummary struct {
	Group        string  `json:"group"`
	SuccessRate  float64 `json:"success_rate"`
	CacheHitRate float64 `json:"cache_hit_rate"`
	RequestCount int64   `json:"-"`
}

type GroupModelSummary struct {
	Group        string  `json:"group"`
	ModelName    string  `json:"model_name"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	AvgTtftMs    int64   `json:"avg_ttft_ms"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTps       float64 `json:"avg_tps"`
	CacheHitRate float64 `json:"cache_hit_rate"`
	RequestCount int64   `json:"-"`
}

// ChannelSummary describes site-wide relay performance for one channel.
type ChannelSummary struct {
	ChannelID    int     `json:"channel_id"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	AvgTtftMs    int64   `json:"avg_ttft_ms"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTps       float64 `json:"avg_tps"`
	CacheHitRate float64 `json:"cache_hit_rate"`
	RequestCount int64   `json:"request_count"`
}

type ChannelSeries struct {
	ChannelID int           `json:"channel_id"`
	Series    []BucketPoint `json:"series"`
}

type GroupModelSeries struct {
	Group        string        `json:"group"`
	ModelName    string        `json:"model_name"`
	SuccessRate  float64       `json:"success_rate"`
	CacheHitRate float64       `json:"cache_hit_rate"`
	RequestCount int64         `json:"request_count"`
	Series       []BucketPoint `json:"series"`
}

type bucketKey struct {
	model    string
	group    string
	bucketTs int64
}

type channelBucketKey struct {
	channelID int
	bucketTs  int64
}

type counters struct {
	requestCount     int64
	successCount     int64
	totalLatencyMs   int64
	ttftSumMs        int64
	ttftCount        int64
	outputTokens     int64
	generationMs     int64
	inputTokens      int64
	cacheReadTokens  int64
	cacheWriteTokens int64
}

type atomicBucket struct {
	requestCount     atomic.Int64
	successCount     atomic.Int64
	totalLatencyMs   atomic.Int64
	ttftSumMs        atomic.Int64
	ttftCount        atomic.Int64
	outputTokens     atomic.Int64
	generationMs     atomic.Int64
	inputTokens      atomic.Int64
	cacheReadTokens  atomic.Int64
	cacheWriteTokens atomic.Int64
}

func (b *atomicBucket) add(sample Sample) {
	b.requestCount.Add(1)
	if sample.Success {
		b.successCount.Add(1)
	}
	if sample.LatencyMs > 0 {
		b.totalLatencyMs.Add(sample.LatencyMs)
	}
	if sample.HasTtft && sample.TtftMs >= 0 {
		b.ttftSumMs.Add(sample.TtftMs)
		b.ttftCount.Add(1)
	}
	if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
		b.outputTokens.Add(sample.OutputTokens)
		b.generationMs.Add(sample.GenerationMs)
	}
	if sample.InputTokens > 0 {
		b.inputTokens.Add(sample.InputTokens)
	}
	if sample.CacheReadTokens > 0 {
		b.cacheReadTokens.Add(sample.CacheReadTokens)
	}
	if sample.CacheWriteTokens > 0 {
		b.cacheWriteTokens.Add(sample.CacheWriteTokens)
	}
}

func (b *atomicBucket) snapshot() counters {
	return counters{
		requestCount:     b.requestCount.Load(),
		successCount:     b.successCount.Load(),
		totalLatencyMs:   b.totalLatencyMs.Load(),
		ttftSumMs:        b.ttftSumMs.Load(),
		ttftCount:        b.ttftCount.Load(),
		outputTokens:     b.outputTokens.Load(),
		generationMs:     b.generationMs.Load(),
		inputTokens:      b.inputTokens.Load(),
		cacheReadTokens:  b.cacheReadTokens.Load(),
		cacheWriteTokens: b.cacheWriteTokens.Load(),
	}
}

func (b *atomicBucket) drain() counters {
	return counters{
		requestCount:     b.requestCount.Swap(0),
		successCount:     b.successCount.Swap(0),
		totalLatencyMs:   b.totalLatencyMs.Swap(0),
		ttftSumMs:        b.ttftSumMs.Swap(0),
		ttftCount:        b.ttftCount.Swap(0),
		outputTokens:     b.outputTokens.Swap(0),
		generationMs:     b.generationMs.Swap(0),
		inputTokens:      b.inputTokens.Swap(0),
		cacheReadTokens:  b.cacheReadTokens.Swap(0),
		cacheWriteTokens: b.cacheWriteTokens.Swap(0),
	}
}

func (b *atomicBucket) addCounters(c counters) {
	if c.requestCount != 0 {
		b.requestCount.Add(c.requestCount)
	}
	if c.successCount != 0 {
		b.successCount.Add(c.successCount)
	}
	if c.totalLatencyMs != 0 {
		b.totalLatencyMs.Add(c.totalLatencyMs)
	}
	if c.ttftSumMs != 0 {
		b.ttftSumMs.Add(c.ttftSumMs)
	}
	if c.ttftCount != 0 {
		b.ttftCount.Add(c.ttftCount)
	}
	if c.outputTokens != 0 {
		b.outputTokens.Add(c.outputTokens)
	}
	if c.generationMs != 0 {
		b.generationMs.Add(c.generationMs)
	}
	if c.inputTokens != 0 {
		b.inputTokens.Add(c.inputTokens)
	}
	if c.cacheReadTokens != 0 {
		b.cacheReadTokens.Add(c.cacheReadTokens)
	}
	if c.cacheWriteTokens != 0 {
		b.cacheWriteTokens.Add(c.cacheWriteTokens)
	}
}
