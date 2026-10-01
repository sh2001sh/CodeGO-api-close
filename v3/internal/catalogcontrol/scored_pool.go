package catalogcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func normalizePool(p *RoutePool) error {
	if p.Strategy == "" && p.Name != "" {
		p.Strategy = "scored" // retained v2 official pool payload
	}
	if p.Strategy == "scored" {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("scored pool name required")
		}
		p.Model = p.ModelScope
		if strings.TrimSpace(p.Model) == "" {
			p.Model = "*"
		}
	} else if p.Name != "" {
		return fmt.Errorf("native pools must be unnamed")
	}
	if p.ID < 0 || p.Group == "" || p.Model == "" || len(p.Group) > 255 || len(p.Model) > 255 || len(p.Name) > 128 || len(p.ModelScope) > 255 || len(p.Members) > 10000 {
		return fmt.Errorf("invalid pool metadata")
	}
	if p.Strategy != "weighted" && p.Strategy != "round_robin" && p.Strategy != "fill_first" && p.Strategy != "scored" {
		return fmt.Errorf("invalid pool strategy")
	}
	for _, weight := range []int{p.MultiplierWeight, p.TTFTWeight, p.CacheWeight, p.SuccessWeight} {
		if weight < 0 || weight > 100 {
			return fmt.Errorf("invalid scoring weight")
		}
	}
	seen, legacyIDs := map[int64]bool{}, map[int64]bool{}
	for i := range p.Members {
		m := &p.Members[i]
		if p.Strategy == "scored" && m.Weight == 0 {
			m.Weight = 1
		}
		if m.CostMultiplier == "" {
			m.CostMultiplier = json.Number("1")
		}
		if m.ModelCostOverrides == nil {
			m.ModelCostOverrides = map[string]json.Number{}
		}
		if m.Enabled == nil {
			enabled := true
			m.Enabled = &enabled
		}
		if m.ChannelID <= 0 || m.Weight <= 0 || seen[m.ChannelID] || m.LegacyID < 0 || (m.LegacyID > 0 && legacyIDs[m.LegacyID]) || len(m.FaultDomain) > 128 || !catalog.PositivePoolDecimal(string(m.CostMultiplier)) {
			return fmt.Errorf("invalid pool member")
		}
		seen[m.ChannelID], legacyIDs[m.LegacyID] = true, true
		for model, cost := range m.ModelCostOverrides {
			if strings.TrimSpace(model) == "" || len(model) > 255 || !catalog.PositivePoolDecimal(string(cost)) {
				return fmt.Errorf("invalid model cost")
			}
		}
	}
	return nil
}

func writePool(ctx context.Context, tx pgx.Tx, p *RoutePool) error {
	// Serialize enabling/saving pools in one group. Disabling the previous pool
	// and replacing members either commit together or roll back together.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('official-route-pool:'||$1,0))`, p.Group); err != nil {
		return err
	}
	if p.Strategy == "scored" && p.Enabled && p.DeletedAt == nil {
		if _, err := tx.Exec(ctx, `UPDATE v3_catalog.route_pools SET enabled=false WHERE group_name=$1 AND strategy='scored' AND id<>$2 AND enabled AND deleted_at IS NULL`, p.Group, p.ID); err != nil {
			return err
		}
	}
	args := []any{p.Group, p.Model, p.Strategy, p.Enabled, p.Name, p.ModelScope, p.AutoDiscover, p.MultiplierWeight, p.TTFTWeight, p.CacheWeight, p.SuccessWeight, p.DeletedAt}
	var err error
	if p.ID == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO v3_catalog.route_pools(group_name,model,strategy,enabled,name,model_scope,auto_discover,multiplier_weight,ttft_weight,cache_weight,success_weight,deleted_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(group_name,model) WHERE name='' DO UPDATE SET strategy=excluded.strategy,enabled=excluded.enabled,name=excluded.name,model_scope=excluded.model_scope,auto_discover=excluded.auto_discover,multiplier_weight=excluded.multiplier_weight,ttft_weight=excluded.ttft_weight,cache_weight=excluded.cache_weight,success_weight=excluded.success_weight,deleted_at=excluded.deleted_at RETURNING id`, args...).Scan(&p.ID)
	} else {
		args = append(args, p.ID)
		err = tx.QueryRow(ctx, `UPDATE v3_catalog.route_pools SET group_name=$1,model=$2,strategy=$3,enabled=$4,name=$5,model_scope=$6,auto_discover=$7,multiplier_weight=$8,ttft_weight=$9,cache_weight=$10,success_weight=$11,deleted_at=$12 WHERE id=$13 RETURNING id`, args...).Scan(&p.ID)
	}
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(p.Members))
	for _, m := range p.Members {
		ids = append(ids, m.ChannelID)
		overrides, err := json.Marshal(m.ModelCostOverrides)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_catalog.route_pool_members(pool_id,channel_id,priority,weight,legacy_id,cost_multiplier,model_cost_overrides,fault_domain,enabled,deleted_at) VALUES($1,$2,$3,$4,nullif($5,0),$6::numeric,$7,$8,$9,$10)
		ON CONFLICT(pool_id,channel_id) DO UPDATE SET priority=excluded.priority,weight=excluded.weight,legacy_id=coalesce(excluded.legacy_id,v3_catalog.route_pool_members.legacy_id),cost_multiplier=excluded.cost_multiplier,model_cost_overrides=excluded.model_cost_overrides,fault_domain=excluded.fault_domain,enabled=excluded.enabled,deleted_at=excluded.deleted_at`, p.ID, m.ChannelID, m.Priority, m.Weight, m.LegacyID, string(m.CostMultiplier), overrides, m.FaultDomain, *m.Enabled, m.DeletedAt); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM v3_catalog.route_pool_members WHERE pool_id=$1 AND channel_id<>ALL($2::bigint[])`, p.ID, ids)
	return err
}
