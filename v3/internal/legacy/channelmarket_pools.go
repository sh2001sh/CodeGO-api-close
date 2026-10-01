package legacy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (d *channelMarketData) preparePools() {
	pools := map[string]cmRow{}
	for _, r := range d.rows["route_pools"] {
		if pools[r.text("id")] != nil {
			d.issue("route_pools", r, errors.New("duplicate source pool ID"))
		}
		pools[r.text("id")] = r
	}
	auto := map[int64]cmRow{}
	for _, r := range d.rows["auto_route_pool_configs"] {
		if auto[cmInt(r, "owner_user_id")] != nil {
			d.issue("auto_route_pool_configs", r, errors.New("duplicate auto pool owner"))
		}
		auto[cmInt(r, "owner_user_id")] = r
	}
	for _, r := range d.rows["auto_route_pool_members"] {
		owner := cmInt(r, "owner_user_id")
		if auto[owner] == nil {
			auto[owner] = cmRow{"owner_user_id": r["owner_user_id"], "strategy": []byte(`"priority"`), "max_attempts": []byte(`3`), "failure_cooldown_seconds": []byte(`30`)}
		}
	}
	for _, key := range d.keyBindings {
		if key.Group == "market:auto" && auto[key.UserID] == nil {
			owner := []byte(fmt.Sprintf("%d", key.UserID))
			auto[key.UserID] = cmRow{"owner_user_id": owner, "strategy": []byte(`"priority"`), "max_attempts": []byte(`3`), "failure_cooldown_seconds": []byte(`30`)}
		}
	}
	for _, r := range pools {
		d.preparePool(r, r.text("id"), r.text("name"), false)
	}
	for owner, r := range auto {
		d.preparePool(r, cmAutoID(owner), "Auto", true)
	}
	for _, table := range []string{"route_pool_members", "auto_route_pool_members"} {
		for _, r := range d.rows[table] {
			b := cmBuild()
			id := r.text("pool_id")
			if table == "auto_route_pool_members" {
				id = cmAutoID(cmInt(r, "owner_user_id"))
			} else if pools[id] == nil {
				b.err = errors.New("unknown named route pool")
			}
			b.put("pool_id", id)
			member := r.text("group_id")
			b.put("group_id", member)
			b.put("catalog_group_name", nil)
			if strings.HasPrefix(member, "official:") {
				group := strings.TrimPrefix(member, "official:")
				found := false
				for _, c := range d.internal {
					for _, g := range list(c.text("group")) {
						if g == group {
							found = true
						}
					}
				}
				if !found {
					b.err = errors.New("official pool member has no source catalog group")
				}
				b.put("catalog_group_name", group)
			} else {
				if _, err := d.group(member); err != nil {
					b.err = err
				}
				b.put("group_id", member)
			}
			priority := b.integer(r, "priority", "priority")
			if priority < -math.MaxInt32 || priority > math.MaxInt32 {
				b.err = errors.New("pool priority outside supported integer range")
			}
			b.integer(r, "id", "legacy_id")
			b.put("legacy_source", table)
			d.record("v3_channelmarket.route_pool_members", []string{"pool_id", "group_id"}, b, r)
		}
	}
}

func (d *channelMarketData) preparePool(r cmRow, id, name string, isAuto bool) {
	b := cmBuild()
	owner := b.integer(r, "owner_user_id", "owner_user_id")
	if err := d.user(owner); err != nil {
		b.err = err
	}
	if id == "" || name == "" {
		b.err = errors.New("pool ID and name required")
	}
	b.put("id", id)
	b.put("name", name)
	b.put("internal_group_name", "pool_"+id)
	strategy := r.text("strategy")
	if strategy == "" {
		strategy = "priority"
	}
	switch strategy {
	case "weighted", "priority", "round_robin", "fill_first", "cost", "score":
	default:
		b.err = fmt.Errorf("unknown route pool strategy %s", strategy)
	}
	b.put("strategy", strategy)
	attempts := b.integer(r, "max_attempts", "max_attempts")
	if attempts == 0 {
		b.put("max_attempts", int64(3))
	} else if attempts < 1 || attempts > 32 {
		b.err = errors.New("max attempts outside 1..32")
	}
	cooldown := b.integer(r, "failure_cooldown_seconds", "failure_cooldown_seconds")
	if cooldown < 0 || cooldown > 3600 {
		b.err = errors.New("cooldown outside 0..3600")
	}
	b.factor(r, "max_multiplier", "max_multiplier_ppm", true)
	config := map[string]any{"legacy_auto": isAuto}
	for _, key := range []string{"multiplier_weight", "success_weight", "cache_weight", "ttft_weight"} {
		if len(r[key]) > 0 {
			config[key] = r[key]
		}
	}
	if !isAuto {
		build := map[string]any{}
		for source, target := range map[string]string{"auto_build_enabled": "enabled", "auto_build_schedule": "schedule", "auto_build_interval": "interval_minutes", "auto_build_daily_time": "daily_time", "auto_build_model": "model", "auto_build_consumer_weight": "consumer_weight", "auto_build_success_weight": "success_weight", "auto_build_ttft_weight": "ttft_weight", "auto_build_cache_weight": "cache_weight", "auto_build_size": "size", "auto_build_explore": "explore", "auto_build_last_error": "last_error", "auto_build_last_at": "last_build_at", "auto_build_next_at": "next_build_at"} {
			if len(r[source]) > 0 {
				build[target] = r[source]
			}
		}
		config["auto_build"] = build
		if len(r["auto_build_last_at"]) > 0 {
			config["last_built_at"] = r["auto_build_last_at"]
		}
	}
	if len(r["auto_build_models"]) > 0 {
		models, err := r.structured("auto_build_models", "[]")
		if err != nil {
			b.err = err
		} else {
			if build, ok := config["auto_build"].(map[string]any); ok {
				build["models"] = models
			}
		}
	}
	b.put("config", config)
	b.times(r, "created_at", "updated_at")
	d.record("v3_channelmarket.route_pools", []string{"id"}, b, r)
}

func (d *channelMarketData) importRouting(ctx context.Context, tx pgx.Tx) error {
	for _, r := range d.records {
		if r.table != "v3_channelmarket.route_pools" {
			continue
		}
		group := r.values["internal_group_name"].(string)
		strategy := r.values["strategy"].(string)
		if strategy == "priority" || strategy == "cost" || strategy == "score" {
			strategy = "fill_first"
		}
		var pool int64
		if err := tx.QueryRow(ctx, `INSERT INTO v3_catalog.route_pools(group_name,model,strategy) VALUES($1,'*',$2) ON CONFLICT(group_name,model) WHERE name='' DO UPDATE SET strategy=EXCLUDED.strategy RETURNING id`, group, strategy).Scan(&pool); err != nil {
			return err
		}
		for _, member := range d.records {
			if member.table != "v3_channelmarket.route_pool_members" || member.values["pool_id"] != r.values["id"] {
				continue
			}
			// Operational pools keep the source ascending ordinal; the catalog
			// routes use the inverse convention, with larger values first.
			priority := -member.values["priority"].(int64)
			if official, ok := member.values["catalog_group_name"].(string); ok {
				if _, err := tx.Exec(ctx, `INSERT INTO v3_catalog.route_pool_members(pool_id,channel_id,priority) SELECT $1,c.channel_id,$3 FROM v3_catalog.channel_groups c WHERE c.group_name=$2 ON CONFLICT DO NOTHING`, pool, official, priority); err != nil {
					return err
				}
			} else {
				g, err := d.group(member.values["group_id"].(string))
				if err != nil {
					return err
				}
				c, err := d.channel(g.text("channel_id"))
				if err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `INSERT INTO v3_catalog.route_pool_members(pool_id,channel_id,priority) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, pool, c.catalogID, priority); err != nil {
					return err
				}
			}
		}
		legacyGroup := "market:pool:" + r.values["id"].(string)
		if strings.HasPrefix(r.values["id"].(string), "auto:") {
			legacyGroup = "market:auto"
		}
		if _, err := tx.Exec(ctx, `UPDATE v3_identity.api_keys SET group_name=$1 WHERE user_id=$2 AND group_name=$3`, group, r.values["owner_user_id"], legacyGroup); err != nil {
			return err
		}
	}
	for _, g := range d.groups {
		if _, err := tx.Exec(ctx, `UPDATE v3_identity.api_keys SET group_name=$1 WHERE group_name=$2`, g.text("internal_group_name"), "market:"+g.text("id")); err != nil {
			return err
		}
	}
	return nil
}
