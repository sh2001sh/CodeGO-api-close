package routing

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
)

type poolCandidate struct {
	member   catalog.MarketPoolMember
	group    string
	factor   string
	score    float64
	hasScore bool
	routes   []catalog.Route
	official map[int64]OfficialSelection
}

// Cost depends on the actor's override and the current multiplier window, so
// this small group ordering is request-local rather than a global cached index.
func (b *planBuilder) orderedPoolIndex(pool catalog.MarketPoolPolicy) *routeIndex {
	now := time.Unix(0, b.now)
	candidates := b.buildPoolCandidates(pool, now)
	sortPoolCandidates(candidates, pool.Strategy)
	routes := b.flattenPoolRoutes(candidates)
	if len(routes) == 0 {
		return nil
	}
	return buildIndex(b.snap, routes)
}

// buildPoolCandidates resolves each pool member's eligibility, cost factor
// and candidate routes for the current request.
func (b *planBuilder) buildPoolCandidates(pool catalog.MarketPoolPolicy, now time.Time) []poolCandidate {
	var candidates []poolCandidate
	for _, member := range pool.Members {
		group := member.CatalogGroupName
		if group == "" && strings.HasPrefix(member.GroupID, "official:") {
			group = strings.TrimPrefix(member.GroupID, "official:")
		}
		c := poolCandidate{member: member, group: group}
		if strings.HasPrefix(member.GroupID, "official:") {
			if !b.allowsGroup(group) {
				continue
			}
			g, exists := b.snap.Groups[group]
			if !exists {
				continue
			}
			c.factor = exactfactor.FromInt64(int64(math.Round(g.Multiplier * 1000000)))
		} else {
			g, exists := b.snap.Market.Groups[group]
			if !exists || g.ID != member.GroupID || !g.Allows(b.req.Principal.UserID) {
				continue
			}
			policy, exists := b.snap.Market.Channels[g.ChannelID]
			if !exists || policy.Blocked[b.req.Principal.UserID] {
				continue
			}
			c.factor = policy.FactorExact(b.req.Principal.UserID, now)
			c.score, c.hasScore = g.Score, g.HasScore
		}
		if !routingFactorAllowed(c.factor, pool.MaxMultiplierPPM) || (!strings.HasPrefix(member.GroupID, "official:") && !routingFactorAllowed(c.factor, b.req.Principal.MaxMarketplaceMultiplierPPM)) {
			continue
		}
		if scored, ok := b.snap.OfficialPools[group]; ok && scored.Matches(b.req.Model) {
			idx, selections := officialPoolIndex(b.snap, scored, b.req.Model, b.p.official, now)
			c.official = selections
			if idx != nil {
				for _, tier := range idx.tiers {
					for _, entry := range tier.entries {
						c.routes = append(c.routes, catalog.Route{ChannelID: entry.ch.ID, Weight: 1})
					}
				}
			}
		} else {
			c.routes = b.snap.Routes[group][b.req.Model]
			if len(c.routes) == 0 {
				c.routes = b.snap.Routes[group]["*"]
			}
		}
		if len(c.routes) > 0 {
			candidates = append(candidates, c)
		}
	}
	return candidates
}

// sortPoolCandidates orders candidates in place per the pool's strategy,
// falling back to GroupID for a stable, deterministic tiebreak.
func sortPoolCandidates(candidates []poolCandidate, strategy string) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, z := candidates[i], candidates[j]
		switch strategy {
		case "priority":
			if a.member.Priority != z.member.Priority {
				return a.member.Priority < z.member.Priority
			}
		case "cost":
			// Candidate factors were validated before this sort. Invalid direct
			// callers fall through to the deterministic identity tiebreak.
			if cmp, err := exactfactor.Compare(a.factor, z.factor); err == nil && cmp != 0 {
				return cmp < 0
			}
		case "score":
			if a.hasScore != z.hasScore {
				return a.hasScore
			}
			if a.hasScore && a.score != z.score {
				return a.score > z.score
			}
		}
		return a.member.GroupID < z.member.GroupID
	})
}

// flattenPoolRoutes merges each candidate's routes into one priority-ordered,
// de-duplicated list, recording group and official-selection bookkeeping on
// b as a side effect.
func (b *planBuilder) flattenPoolRoutes(candidates []poolCandidate) []catalog.Route {
	var routes []catalog.Route
	seen := map[int64]bool{}
	b.poolGroups, b.poolOrdered = map[int64]string{}, true
	priority := len(candidates)
	for _, c := range candidates {
		priority += len(c.routes)
	}
	for _, c := range candidates {
		for ordinal, route := range c.routes {
			if seen[route.ChannelID] {
				continue
			}
			seen[route.ChannelID] = true
			route.Priority, route.Strategy = priority, "fill_first"
			if c.official != nil {
				route.Priority -= ordinal
			}
			routes = append(routes, route)
			b.poolGroups[route.ChannelID] = c.group
			if selection, scored := c.official[route.ChannelID]; scored {
				if b.officialSelections == nil {
					b.officialSelections = map[int64]OfficialSelection{}
				}
				b.officialSelections[route.ChannelID] = selection
				b.enableOfficialIsolation()
			}
		}
		priority -= len(c.routes) + 1
	}
	return routes
}
