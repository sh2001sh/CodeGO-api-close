package runtime

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestFirstByteTraceSnapshotSeparatesRequestStages(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	trace := NewFirstByteTrace(startedAt)
	trace.requestValidAt = startedAt.Add(10 * time.Millisecond)
	trace.admittedAt = startedAt.Add(15 * time.Millisecond)
	trace.relayInfoReadyAt = startedAt.Add(20 * time.Millisecond)
	trace.preflightDoneAt = startedAt.Add(40 * time.Millisecond)
	trace.routeSelectedAt = startedAt.Add(65 * time.Millisecond)
	trace.upstreamStartAt = startedAt.Add(80 * time.Millisecond)
	trace.firstEventAt = startedAt.Add(900 * time.Millisecond)
	trace.firstSemanticReadAt = startedAt.Add(940 * time.Millisecond)
	trace.firstSemanticAt = startedAt.Add(950 * time.Millisecond)
	trace.firstTextReadAt = startedAt.Add(945 * time.Millisecond)
	trace.firstTextAt = startedAt.Add(951 * time.Millisecond)
	trace.firstSemanticIsText = true
	trace.semanticKindMarked = true

	require.Equal(t, map[string]int64{
		"ingress_ms":                       20,
		"request_validation_ms":            10,
		"admission_ms":                     5,
		"relay_info_ms":                    5,
		"preflight_ms":                     20,
		"route_selection_ms":               25,
		"dispatch_ms":                      15,
		"upstream_first_event_ms":          820,
		"event_to_semantic_ms":             50,
		"upstream_first_semantic_read_ms":  860,
		"semantic_read_to_handler_ms":      10,
		"first_semantic_is_text":           1,
		"upstream_first_text_read_ms":      865,
		"text_read_to_handler_ms":          6,
		"upstream_first_semantic_event_ms": 870,
		"total_raw_event_ms":               900,
		"total_ms":                         950,
	}, trace.Snapshot())
}

func TestFirstByteTraceSeparatesScannerReadFromHandler(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	trace := NewFirstByteTrace(startedAt)
	trace.upstreamStartAt = startedAt.Add(100 * time.Millisecond)
	trace.MarkFirstSemanticReadAt(startedAt.Add(800*time.Millisecond), false)
	trace.firstSemanticAt = startedAt.Add(825 * time.Millisecond)

	snapshot := trace.Snapshot()
	require.Equal(t, int64(700), snapshot["upstream_first_semantic_read_ms"])
	require.Equal(t, int64(25), snapshot["semantic_read_to_handler_ms"])
	require.Equal(t, int64(0), snapshot["first_semantic_is_text"])
}

func TestFirstByteTraceSeparatesTextFromOtherDeltas(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	trace := NewFirstByteTrace(startedAt)
	trace.upstreamStartAt = startedAt.Add(100 * time.Millisecond)
	trace.firstSemanticAt = startedAt.Add(600 * time.Millisecond)
	trace.MarkFirstSemanticReadAt(startedAt.Add(550*time.Millisecond), false)
	trace.MarkFirstTextReadAt(startedAt.Add(800 * time.Millisecond))
	trace.firstTextAt = startedAt.Add(810 * time.Millisecond)

	snapshot := trace.Snapshot()
	require.Equal(t, int64(0), snapshot["first_semantic_is_text"])
	require.Equal(t, int64(700), snapshot["upstream_first_text_read_ms"])
	require.Equal(t, int64(10), snapshot["text_read_to_handler_ms"])
}

func TestFirstByteTraceReturnsNilWithoutAnUpstreamEvent(t *testing.T) {
	require.Nil(t, NewFirstByteTrace(time.Now()).Snapshot())
}

func TestRelayInfoMarksTraceForFirstSemanticResponse(t *testing.T) {
	startedAt := time.Now().Add(-time.Second)
	info := &RelayInfo{StartTime: startedAt, FirstByteTrace: NewFirstByteTrace(startedAt)}

	info.SetFirstSemanticResponseTime()

	require.NotNil(t, info.FirstByteTrace.Snapshot())
}

func TestRelayInfoSeparatesResponsesLifecycleAndSemanticOutput(t *testing.T) {
	startedAt := time.Now().Add(-time.Second)
	info := &RelayInfo{
		StartTime:       startedAt,
		RelayFormat:     types.RelayFormatOpenAIResponses,
		FirstByteTrace:  NewFirstByteTrace(startedAt),
		isFirstResponse: true,
	}
	info.FirstByteTrace.MarkUpstreamStart()

	info.SetFirstResponseTime()
	require.Nil(t, info.FirstByteTrace.Snapshot())

	info.SetFirstSemanticResponseTime()
	trace := info.FirstByteTrace.Snapshot()
	require.NotNil(t, trace)
	require.GreaterOrEqual(t, trace["event_to_semantic_ms"], int64(0))
}
