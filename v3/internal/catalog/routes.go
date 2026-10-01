package catalog

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

// channelGroupSet maps a channel id to the groups it serves.
type channelGroupSet map[int64]map[string]bool

func loadChannelGroups(ctx context.Context, tx pgx.Tx) (channelGroupSet, error) {
	rows, err := tx.Query(ctx, `SELECT channel_id, group_name FROM v3_catalog.channel_groups`)
	if err != nil {
		return nil, fmt.Errorf("catalog: query channel_groups: %w", err)
	}
	defer rows.Close()

	set := make(channelGroupSet)
	for rows.Next() {
		var channelID int64
		var group string
		if err := rows.Scan(&channelID, &group); err != nil {
			return nil, fmt.Errorf("catalog: scan channel_group: %w", err)
		}
		if set[channelID] == nil {
			set[channelID] = make(map[string]bool)
		}
		set[channelID][group] = true
	}
	return set, rows.Err()
}

// channelModelSet maps a channel id to the models it serves.
type channelModelSet map[int64]map[string]bool

func loadChannelModels(ctx context.Context, tx pgx.Tx) (channelModelSet, error) {
	rows, err := tx.Query(ctx, `SELECT channel_id, model FROM v3_catalog.channel_models`)
	if err != nil {
		return nil, fmt.Errorf("catalog: query channel_models: %w", err)
	}
	defer rows.Close()

	set := make(channelModelSet)
	for rows.Next() {
		var channelID int64
		var model string
		if err := rows.Scan(&channelID, &model); err != nil {
			return nil, fmt.Errorf("catalog: scan channel_model: %w", err)
		}
		if set[channelID] == nil {
			set[channelID] = make(map[string]bool)
		}
		set[channelID][model] = true
	}
	return set, rows.Err()
}

// pool is one enabled route_pool with its members, keyed by (group, model).
type pool struct {
	strategy string
	members  []Route
}

func loadRoutePools(ctx context.Context, tx pgx.Tx) (map[string]map[string]pool, error) {
	rows, err := tx.Query(ctx, `
		SELECT rp.group_name, rp.model, rp.strategy, m.channel_id, m.priority, m.weight
		FROM v3_catalog.route_pools rp
		JOIN v3_catalog.route_pool_members m ON m.pool_id = rp.id
		WHERE rp.enabled = true AND rp.deleted_at IS NULL AND rp.strategy <> 'scored'
		  AND m.enabled = true AND m.deleted_at IS NULL
		ORDER BY rp.group_name, rp.model, m.channel_id`)
	if err != nil {
		return nil, fmt.Errorf("catalog: query route_pools: %w", err)
	}
	defer rows.Close()

	pools := make(map[string]map[string]pool)
	for rows.Next() {
		var group, model, strategy string
		var route Route
		if err := rows.Scan(&group, &model, &strategy, &route.ChannelID, &route.Priority, &route.Weight); err != nil {
			return nil, fmt.Errorf("catalog: scan route_pool member: %w", err)
		}
		route.Weight = clampWeight(route.Weight)
		route.Strategy = strategy
		if pools[group] == nil {
			pools[group] = make(map[string]pool)
		}
		p := pools[group][model]
		p.strategy = strategy
		p.members = append(p.members, route)
		pools[group][model] = p
	}
	return pools, rows.Err()
}

// collectKnownModels gathers every model named by a channel membership or a
// non-wildcard route pool entry.
func collectKnownModels(cm channelModelSet, pools map[string]map[string]pool) map[string]bool {
	models := make(map[string]bool)
	for _, ms := range cm {
		for m := range ms {
			models[m] = true
		}
	}
	for _, groupPools := range pools {
		for model := range groupPools {
			if model != "*" {
				models[model] = true
			}
		}
	}
	return models
}

// applySpecificPools writes every (group, model) route pool directly into
// routes, including wildcard pools stored under model "*" itself.
func applySpecificPools(routes map[string]map[string][]Route, pools map[string]map[string]pool) {
	for group, groupPools := range pools {
		for model, p := range groupPools {
			// A wildcard pool is also stored under "*" itself so requests for
			// models no channel lists explicitly still route (the planner
			// falls back to Routes[group]["*"]). Known models get it below.
			putRoutes(routes, group, model, p.members)
		}
	}
}

// channelMembershipCandidates lists the weighted route candidates for
// (group, model) from plain channel group/model membership, in channel id
// order.
func channelMembershipCandidates(channels map[int64]*Channel, cg channelGroupSet, cm channelModelSet, groupOrder []int64, group, model string) []Route {
	var candidates []Route
	for _, id := range groupOrder {
		if !cg[id][group] || !cm[id][model] {
			continue
		}
		c := channels[id]
		candidates = append(candidates, Route{
			ChannelID: c.ID,
			Priority:  c.Priority,
			Weight:    clampWeight(c.Weight),
			Strategy:  "weighted",
		})
	}
	return candidates
}

// applyFallbackRoutes fills in every (group, model) pair not already covered
// by a specific pool, falling back to the group's wildcard pool and then to
// plain channel membership.
func applyFallbackRoutes(routes map[string]map[string][]Route, channels map[int64]*Channel, cg channelGroupSet, cm channelModelSet, pools map[string]map[string]pool, models map[string]bool) {
	var groupOrder []int64
	for id := range channels {
		groupOrder = append(groupOrder, id)
	}
	sort.Slice(groupOrder, func(i, j int) bool { return groupOrder[i] < groupOrder[j] })

	for group := range groupsOf(cg, pools) {
		for model := range models {
			if _, ok := routes[group][model]; ok {
				continue // specific pool already set it
			}
			if wildcard, ok := pools[group]["*"]; ok {
				putRoutes(routes, group, model, wildcard.members)
				continue
			}
			if model == "*" {
				continue
			}
			if candidates := channelMembershipCandidates(channels, cg, cm, groupOrder, group, model); len(candidates) > 0 {
				putRoutes(routes, group, model, candidates)
			}
		}
	}
}

// buildRoutes indexes candidates by group then model. When an enabled route
// pool exists for (group, model) or (group, '*'), its members are used
// as-is; otherwise every enabled channel that belongs to the group and lists
// the model is a "weighted" candidate.
func buildRoutes(channels map[int64]*Channel, cg channelGroupSet, cm channelModelSet, pools map[string]map[string]pool) map[string]map[string][]Route {
	routes := make(map[string]map[string][]Route)
	models := collectKnownModels(cm, pools)
	applySpecificPools(routes, pools)
	applyFallbackRoutes(routes, channels, cg, cm, pools, models)
	return routes
}

func putRoutes(routes map[string]map[string][]Route, group, model string, members []Route) {
	if routes[group] == nil {
		routes[group] = make(map[string][]Route)
	}
	routes[group][model] = members
}

func groupsOf(cg channelGroupSet, pools map[string]map[string]pool) map[string]bool {
	groups := make(map[string]bool)
	for _, gs := range cg {
		for g := range gs {
			groups[g] = true
		}
	}
	for g := range pools {
		groups[g] = true
	}
	return groups
}

func clampWeight(w int) int {
	if w < 1 {
		return 1
	}
	return w
}
