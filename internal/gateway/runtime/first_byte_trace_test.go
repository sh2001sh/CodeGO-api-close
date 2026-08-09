package runtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFirstByteTraceSnapshotSeparatesRequestStages(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	trace := NewFirstByteTrace(startedAt)
	trace.relayInfoReadyAt = startedAt.Add(10 * time.Millisecond)
	trace.preflightDoneAt = startedAt.Add(30 * time.Millisecond)
	trace.routeSelectedAt = startedAt.Add(55 * time.Millisecond)
	trace.upstreamStartAt = startedAt.Add(70 * time.Millisecond)
	trace.firstEventAt = startedAt.Add(900 * time.Millisecond)

	require.Equal(t, map[string]int64{
		"ingress_ms":              10,
		"preflight_ms":            20,
		"route_selection_ms":      25,
		"dispatch_ms":             15,
		"upstream_first_event_ms": 830,
		"total_ms":                900,
	}, trace.Snapshot())
}

func TestFirstByteTraceReturnsNilWithoutAnUpstreamEvent(t *testing.T) {
	require.Nil(t, NewFirstByteTrace(time.Now()).Snapshot())
}
