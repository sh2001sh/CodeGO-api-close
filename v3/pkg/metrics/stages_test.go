package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func scrape(t *testing.T, r *Recorder) string {
	t.Helper()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	return string(body)
}

func TestTimelineRecordsStagesAndOverheadOnce(t *testing.T) {
	r := New()
	t0 := time.Unix(0, 0)
	tl := r.Start(t0)
	tl.Mark(StageParse, t0.Add(100*time.Microsecond))
	tl.Mark(StageAuthorize, t0.Add(300*time.Microsecond))
	tl.MarkUpstreamSent(t0.Add(2 * time.Millisecond))
	tl.MarkUpstreamSent(t0.Add(9 * time.Millisecond)) // retry must not count again

	out := scrape(t, r)
	for _, want := range []string{
		`codego_gateway_stage_seconds_count{stage="parse"} 1`,
		`codego_gateway_stage_seconds_count{stage="authorize"} 1`,
		`codego_gateway_stage_seconds_sum{stage="authorize"} 0.0002`,
		`codego_gateway_overhead_seconds_count 1`,
		`codego_gateway_overhead_seconds_sum 0.002`,
		`go_goroutines`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestZeroTimelineIsNoop(t *testing.T) {
	var tl Timeline
	tl.Mark(StageParse, time.Now())
	tl.MarkUpstreamSent(time.Now())
}

func TestAcceptanceThresholdSeparatesOverBudgetRequests(t *testing.T) {
	r := New()
	t0 := time.Unix(0, 0)
	for _, duration := range []time.Duration{19 * time.Millisecond, 21 * time.Millisecond} {
		timeline := r.Start(t0)
		timeline.MarkUpstreamSent(t0.Add(duration))
	}
	if !strings.Contains(scrape(t, r), `codego_gateway_overhead_seconds_bucket{le="0.02"} 1`) {
		t.Fatal("the 20ms acceptance threshold must exclude the over-budget request")
	}
}

func TestMarkDoesNotAllocate(t *testing.T) {
	r := New()
	now := time.Now()
	tl := r.Start(now)
	allocs := testing.AllocsPerRun(1000, func() {
		tl.Mark(StagePlan, now)
	})
	if allocs != 0 {
		t.Fatalf("Mark allocates %.1f times per call; want 0", allocs)
	}
}

func TestStageNames(t *testing.T) {
	if StageUpstreamHeaders.String() != "upstream_headers" || Stage(200).String() != "unknown" {
		t.Fatal("unexpected stage names")
	}
}
