// Package metrics records per-stage gateway latency as Prometheus histograms.
//
// Acceptance criteria 1-3 and the cutover gates (plan §9) are judged from these
// numbers, so the hot path must not allocate when recording them.
package metrics

import (
	"net/http"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Stage is one step of the gateway pipeline (plan §3).
type Stage uint8

const (
	StageParse Stage = iota
	StageAuthorize
	StagePlan
	StageReserve
	StageUpstreamHeaders // request sent -> upstream response headers
	StageFirstEvent      // headers -> first event written to the client
	StageStream          // first event -> last event
	StageFinalize
	StageLimits
	stageCount
)

var stageNames = [stageCount]string{
	"parse", "authorize", "plan", "reserve",
	"upstream_headers", "first_event", "stream", "finalize", "limits",
}

// String returns the Prometheus label value for the stage.
func (s Stage) String() string {
	if s >= stageCount {
		return "unknown"
	}
	return stageNames[s]
}

// Recorder owns a private registry so tests and multiple binaries never
// collide on the global default registry.
type Recorder struct {
	registry  *prometheus.Registry
	observers [stageCount]prometheus.Observer
	overhead  prometheus.Observer
}

// New creates a Recorder with Go runtime and process collectors registered.
func New() *Recorder {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	stages := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "codego",
		Subsystem: "gateway",
		Name:      "stage_seconds",
		Help:      "Latency of each gateway pipeline stage.",
		// 50µs .. ~105s; covers both local stages and upstream waits.
		Buckets: acceptanceBuckets(22),
	}, []string{"stage"})
	overhead := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "codego",
		Subsystem: "gateway",
		Name:      "overhead_seconds",
		Help:      "Gateway time from request received to upstream request sent (acceptance #2).",
		Buckets:   acceptanceBuckets(16),
	})
	reg.MustRegister(stages, overhead)

	r := &Recorder{registry: reg, overhead: overhead}
	for s := Stage(0); s < stageCount; s++ {
		r.observers[s] = stages.WithLabelValues(s.String()) // resolved once, no per-request lookup
	}
	return r
}

func acceptanceBuckets(count int) []float64 {
	buckets := append(prometheus.ExponentialBuckets(0.00005, 2, count), 0.005, 0.020)
	slices.Sort(buckets)
	return buckets
}

// Handler serves the /metrics endpoint.
func (r *Recorder) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}

// Registry exposes the registry for components that add their own metrics.
func (r *Recorder) Registry() *prometheus.Registry { return r.registry }

// Timeline measures consecutive stages of one request. It is a value type so
// it can live inside the request struct without a heap allocation.
type Timeline struct {
	rec      *Recorder
	start    time.Time
	last     time.Time
	overhead bool
}

// Start begins a timeline at now.
func (r *Recorder) Start(now time.Time) Timeline {
	return Timeline{rec: r, start: now, last: now}
}

// Mark records the time since the previous mark as the given stage.
func (t *Timeline) Mark(stage Stage, now time.Time) {
	if t.rec == nil || stage >= stageCount {
		return
	}
	t.rec.observers[stage].Observe(now.Sub(t.last).Seconds())
	t.last = now
}

// MarkUpstreamSent records gateway overhead once, at the moment the upstream
// request is written. Retries do not add to it.
func (t *Timeline) MarkUpstreamSent(now time.Time) {
	if t.rec == nil || t.overhead {
		return
	}
	t.rec.overhead.Observe(now.Sub(t.start).Seconds())
	t.overhead = true
	t.last = now
}
