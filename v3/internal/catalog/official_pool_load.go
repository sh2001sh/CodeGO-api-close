package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

func loadOfficialPools(ctx context.Context, tx pgx.Tx, channels map[int64]*Channel, cg channelGroupSet, cm channelModelSet) (map[string]OfficialPool, error) {
	rows, err := tx.Query(ctx, `SELECT id,name,group_name,model_scope,auto_discover,
		multiplier_weight,ttft_weight,cache_weight,success_weight
		FROM v3_catalog.route_pools WHERE strategy='scored' AND enabled AND deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("catalog: official pools: %w", err)
	}
	pools := map[string]OfficialPool{}
	for rows.Next() {
		var p OfficialPool
		if err = rows.Scan(&p.ID, &p.Name, &p.Group, &p.ModelScope, &p.AutoDiscover, &p.MultiplierWeight, &p.TTFTWeight, &p.CacheWeight, &p.SuccessWeight); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: official pool: %w", err)
		}
		if _, exists := pools[p.Group]; exists {
			rows.Close()
			return nil, fmt.Errorf("catalog: multiple enabled official pools for group %q", p.Group)
		}
		pools[p.Group] = p
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for group, p := range pools {
		members, err := loadOfficialMembers(ctx, tx, p.ID)
		if err != nil {
			return nil, err
		}
		p.Members = compileOfficialMembers(p, members, channels, cg, cm)
		pools[group] = p
	}
	return pools, nil
}

type officialMemberRow struct {
	OfficialPoolMember
	Enabled bool
	Deleted bool
}

func loadOfficialMembers(ctx context.Context, tx pgx.Tx, poolID int64) (map[int64]officialMemberRow, error) {
	rows, err := tx.Query(ctx, `SELECT channel_id,cost_multiplier::text,model_cost_overrides,fault_domain,enabled,deleted_at IS NOT NULL FROM v3_catalog.route_pool_members WHERE pool_id=$1 ORDER BY channel_id`, poolID)
	if err != nil {
		return nil, fmt.Errorf("catalog: official members: %w", err)
	}
	defer rows.Close()
	members := map[int64]officialMemberRow{}
	for rows.Next() {
		var m officialMemberRow
		var overrides []byte
		if err = rows.Scan(&m.ChannelID, &m.CostMultiplier, &overrides, &m.FaultDomain, &m.Enabled, &m.Deleted); err != nil {
			return nil, fmt.Errorf("catalog: official member: %w", err)
		}
		if err = json.Unmarshal(overrides, &m.ModelCostOverrides); err != nil {
			return nil, fmt.Errorf("catalog: official cost overrides: %w", err)
		}
		for model, cost := range m.ModelCostOverrides {
			if model == "" || !PositivePoolDecimal(string(cost)) {
				return nil, fmt.Errorf("catalog: invalid official model cost")
			}
		}
		members[m.ChannelID] = m
	}
	return members, rows.Err()
}

func compileOfficialMembers(p OfficialPool, members map[int64]officialMemberRow, channels map[int64]*Channel, cg channelGroupSet, cm channelModelSet) []OfficialPoolMember {
	var ids []int64
	for id, ch := range channels {
		if ch == nil || ch.Scope != "official" || !cg[id][p.Group] {
			continue
		}
		if _, configured := members[id]; configured || p.AutoDiscover {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]OfficialPoolMember, 0, len(ids))
	for _, id := range ids {
		row, configured := members[id]
		if configured && (!row.Enabled || row.Deleted) {
			continue // explicit disable/deletion blocks rediscovery
		}
		m := row.OfficialPoolMember
		if !configured {
			m.ChannelID, m.CostMultiplier = id, "1"
		}
		for model := range cm[id] {
			if p.Matches(model) {
				m.Models = append(m.Models, model)
			}
		}
		sort.Strings(m.Models)
		if len(m.Models) > 0 {
			out = append(out, m)
		}
	}
	return out
}
