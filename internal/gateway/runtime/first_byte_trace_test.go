package runtime

import (
	"testing"
	"time"

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

	require.Equal(t, map[string]int64{
		"ingress_ms":              20,
		"request_validation_ms":   10,
		"admission_ms":            5,
		"relay_info_ms":           5,
		"preflight_ms":            20,
		"route_selection_ms":      25,
		"dispatch_ms":             15,
		"upstream_first_event_ms": 820,
		"total_ms":                900,
	}, trace.Snapshot())
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
