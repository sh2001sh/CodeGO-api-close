package app

import (
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
)

type autoRouteLiveState struct {
	shared gatewayruntime.ChannelHealth
	user   gatewayruntime.ChannelHealth
}

// PrioritizeAutoRouteBindings preserves the configured order within healthy
// routes. Live distress outranks price/priority, but never changes membership,
// model authorization, billing bindings, or the gateway admission checks.
func PrioritizeAutoRouteBindings(c *gin.Context, bindings []RoutingBinding, model string) []RoutingBinding {
	if len(bindings) < 2 {
		return bindings
	}
	ids := make([]int, 0, len(bindings))
	states := make(map[int]*autoRouteLiveState, len(bindings))
	for _, binding := range bindings {
		if binding.InternalChannelID > 0 && states[binding.InternalChannelID] == nil {
			ids = append(ids, binding.InternalChannelID)
			states[binding.InternalChannelID] = &autoRouteLiveState{}
		}
	}
	if len(ids) == 0 {
		return bindings
	}
	requestType := gatewayruntime.RequestTypeFromContext(c)
	var wg sync.WaitGroup
	for _, id := range ids {
		state := states[id]
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			state.shared, _ = gatewayruntime.GetChannelHealth(id, model, requestType)
		}(id)
		go func(id int) {
			defer wg.Done()
			state.user, _ = gatewayruntime.GetUserChannelHealth(c, id, model, requestType)
		}(id)
	}
	active := gatewayruntime.ActiveChannelRequestsForChannels(ids)
	wg.Wait()
	ordered, reasons := prioritizeAutoRouteBindingsSnapshot(bindings, active, states, time.Now())
	for index, binding := range bindings {
		if reason := reasons[binding.InternalChannelID]; reason != "" {
			gatewayruntime.RecordAutoGroupLiveSignal(c, binding.InternalGroup, binding.InternalChannelID,
				max(0, active[binding.InternalChannelID]), binding.MaxConcurrency, reason)
		}
		if ordered[index].InternalGroup != binding.InternalGroup {
			gatewayruntime.ExcludeRouteDecisionCandidate(c, "auto_groups_reordered_live_state")
		}
	}
	return ordered
}

func prioritizeAutoRouteBindingsSnapshot(bindings []RoutingBinding, active map[int]int, states map[int]*autoRouteLiveState, now time.Time) ([]RoutingBinding, map[int]string) {
	loads := make([]int, 0, len(states))
	latencies := make([]float64, 0, len(states))
	for id, state := range states {
		loads = append(loads, max(0, active[id]))
		if state.shared.TTFTSamples >= 5 && now.Sub(state.shared.LastSuccessAt) < 5*time.Minute && state.shared.TTFTP95Milliseconds > 0 {
			latencies = append(latencies, state.shared.TTFTP95Milliseconds)
		}
	}
	sort.Ints(loads)
	sort.Float64s(latencies)
	medianLoad := 0
	medianLatency := 0.0
	if len(loads) > 0 {
		medianLoad = loads[(len(loads)-1)/2]
	}
	if len(latencies) > 0 {
		medianLatency = latencies[(len(latencies)-1)/2]
	}
	tiers := make(map[int]int, len(states))
	reasons := make(map[int]string, len(states))
	for _, binding := range bindings {
		id := binding.InternalChannelID
		state := states[id]
		if state == nil {
			continue
		}
		inflight := max(0, active[id])
		if (binding.MaxConcurrency > 0 && inflight*100 >= binding.MaxConcurrency*75) ||
			(inflight >= 4 && inflight >= medianLoad*2+2) {
			tiers[id], reasons[id] = 1, "high_inflight"
		}
		h := state.shared
		if h.TTFTSamples >= 5 && now.Sub(h.LastSuccessAt) < 5*time.Minute && h.TTFTP95Milliseconds > max(15000, medianLatency*2) {
			tiers[id], reasons[id] = 1, "slow_recent_ttft"
		}
		if h.Window2Requests >= 5 && now.Sub(h.Window2StartedAt) < 2*time.Minute && h.Window2Successes*100 < h.Window2Requests*90 {
			tiers[id], reasons[id] = 2, "recent_upstream_failures"
		}
		u := state.user
		if u.LastFailureAt.After(u.LastSuccessAt) && now.Sub(u.LastFailureAt) < 30*time.Second {
			tiers[id], reasons[id] = 2, "recent_user_route_failure"
		}
		if u.CoolingUntil.After(now) || u.RecoveryProbeUntil.After(now) {
			tiers[id], reasons[id] = 3, "user_route_cooling"
		}
	}
	ordered := append([]RoutingBinding(nil), bindings...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return tiers[ordered[i].InternalChannelID] < tiers[ordered[j].InternalChannelID]
	})
	return ordered, reasons
}
