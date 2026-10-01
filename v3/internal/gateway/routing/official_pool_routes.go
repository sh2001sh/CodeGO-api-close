package routing

import (
	"math/big"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// OfficialSelection freezes procurement facts with the request's route plan.
// These multipliers order procurement routes; they are not customer pricing.
type OfficialSelection struct {
	PoolID         int64
	CostMultiplier string
	FaultDomain    string
}

type officialCandidate struct {
	member catalog.OfficialPoolMember
	ch     *catalog.Channel
	health officialStats
	score  float64
	ttft   float64
}

func officialPoolIndex(snap *catalog.Snapshot, pool catalog.OfficialPool, model string, health *officialHealth, now time.Time) (*routeIndex, map[int64]OfficialSelection) {
	if !pool.Matches(model) {
		return nil, nil
	}
	var candidates []officialCandidate
	var latencies []float64
	for _, member := range pool.Members {
		ch := snap.Channels[member.ChannelID]
		if ch == nil || len(ch.Credentials) == 0 || !slices.Contains(member.Models, model) {
			continue
		}
		state := health.read(ch.ID, ch.UpstreamModel(model), now)
		c := officialCandidate{member: member, ch: ch, health: state, ttft: state.p95(now)}
		c.score = officialScore(pool, member.Cost(model), state, now)
		candidates = append(candidates, c)
		if c.ttft > 0 {
			latencies = append(latencies, c.ttft)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		median := latencies[(len(latencies)-1)/2]
		for i := range candidates {
			if ratio := candidates[i].ttft / median; ratio > 2.5 {
				candidates[i].score *= 2
			} else if ratio > 1.5 {
				candidates[i].score *= 1.35
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, z := candidates[i], candidates[j]
		if x, y := officialReliabilityTier(a.health), officialReliabilityTier(z.health); x != y {
			return x < y
		}
		if a.score != z.score {
			return a.score < z.score
		}
		// Retain exact-decimal ordering when float scores round to the same value.
		x, xok := new(big.Rat).SetString(a.member.Cost(model))
		y, yok := new(big.Rat).SetString(z.member.Cost(model))
		if xok && yok && x.Cmp(y) != 0 {
			return x.Cmp(y) < 0
		}
		return a.ch.ID < z.ch.ID
	})
	routes := make([]catalog.Route, 0, len(candidates))
	selections := map[int64]OfficialSelection{}
	for i, candidate := range candidates {
		id := candidate.ch.ID
		routes = append(routes, catalog.Route{ChannelID: id, Priority: len(candidates) - i, Weight: 1, Strategy: "fill_first"})
		selections[id] = OfficialSelection{pool.ID, candidate.member.Cost(model), officialFaultDomain(candidate.member, candidate.ch)}
	}
	return buildIndex(snap, routes), selections
}

func officialFaultDomain(member catalog.OfficialPoolMember, ch *catalog.Channel) string {
	if configured := strings.ToLower(strings.TrimSpace(member.FaultDomain)); configured != "" {
		return configured
	}
	if upstream, err := url.Parse(ch.BaseURL); err == nil && upstream.Hostname() != "" {
		return strings.ToLower(ch.Provider + ":" + upstream.Hostname())
	}
	return ch.Provider + ":channel:" + strconv.FormatInt(ch.ID, 10)
}

func officialDomainAllowed(selections map[int64]OfficialSelection, targets []gateway.Target, channelID int64) bool {
	selection, scored := selections[channelID]
	if !scored {
		return true
	}
	for _, target := range targets {
		if target.ChannelID == channelID {
			return false
		}
		if previous, ok := selections[target.ChannelID]; ok && previous.FaultDomain == selection.FaultDomain {
			return false
		}
	}
	return true
}
