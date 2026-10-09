package legacy

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
)

func (m *Importer) importChannelMarket(ctx context.Context, target pgx.Tx, data *channelMarketData) error {
	if data == nil {
		return nil
	}
	if len(data.issues) > 0 {
		return errors.New("legacy: channel market import blocked by validation issues")
	}
	ids := make([]string, 0, len(data.channels))
	for id := range data.channels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c := data.channels[id]
		g := c.group
		factor, err := cmPublicFactor(g.text("multiplier"))
		if err != nil {
			return err
		}
		if _, err = target.Exec(ctx, `INSERT INTO v3_catalog.groups(name,description,multiplier) VALUES($1,$2,$3::numeric/1000000) ON CONFLICT(name) DO UPDATE SET description=EXCLUDED.description,multiplier=EXCLUDED.multiplier`, g.text("internal_group_name"), g.text("system_display_name"), factor); err != nil {
			return err
		}
		if c.archiveParent {
			// Preserve deleted history and its FKs without restoring a usable
			// upstream or retaining any credential for the removed parent.
			var archived bool
			err := target.QueryRow(ctx, `SELECT scope='marketplace' AND owner_user_id=$2 AND status='disabled' AND base_url=''
			 AND settings->'community'->>'id'=$3
			 AND NOT EXISTS(SELECT 1 FROM v3_catalog.channel_credentials k WHERE k.channel_id=c.id)
			 FROM v3_catalog.channels c WHERE id=$1`, c.catalogID, c.owner, id).Scan(&archived)
			if errors.Is(err, pgx.ErrNoRows) {
				_, err = target.Exec(ctx, `INSERT INTO v3_catalog.channels(id,name,provider,base_url,status,scope,owner_user_id,max_concurrency,max_user_concurrency,multiplier_card_supported,settings)
				 VALUES($1,$2,$3,'','disabled','marketplace',$4,$5,$6,$7,$8)`, c.catalogID, g.text("system_display_name"), rProvider(c.row.text("provider_type")), c.owner, cmInt(c.row, "max_concurrency"), cmInt(c.row, "user_max_concurrency"), c.row.text("multiplier_card_supported") == "true", c.settings)
			} else if err == nil && !archived {
				err = errors.New("legacy: deleted marketplace parent conflicts with target channel")
			}
			if err != nil {
				return err
			}
		} else if c.newCatalog {
			sealed, err := m.crypto.Encrypt([]byte(c.credential))
			if err != nil {
				return err
			}
			status := "disabled"
			if state, e := cmLifecycle(g.text("lifecycle_status")); e == nil && state == "active" && g.text("deleted_at") == "" {
				status = "enabled"
			}
			var existing int64
			err = target.QueryRow(ctx, `SELECT channel_id FROM v3_channelmarket.groups WHERE public_channel_id=$1`, id).Scan(&existing)
			if err == nil {
				if existing != c.catalogID {
					return errors.New("legacy: channel public ID mapping changed")
				}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			} else {
				_, err = target.Exec(ctx, `INSERT INTO v3_catalog.channels(id,name,provider,base_url,status,scope,owner_user_id,max_concurrency,max_user_concurrency,multiplier_card_supported,settings) VALUES($1,$2,$3,$4,$5,'marketplace',$6,$7,$8,$9,$10)`, c.catalogID, g.text("system_display_name"), rProvider(c.row.text("provider_type")), c.url, status, c.owner, cmInt(c.row, "max_concurrency"), cmInt(c.row, "user_max_concurrency"), c.row.text("multiplier_card_supported") == "true", c.settings)
				if err != nil {
					return err
				}
				if _, err = target.Exec(ctx, `INSERT INTO v3_catalog.channel_credentials(channel_id,secret,kind,expires_at) VALUES($1,$2,$3,$4)`, c.catalogID, sealed, c.credentialKind, c.credentialExpiry); err != nil {
					return err
				}
			}
		} else {
			tag, err := target.Exec(ctx, `UPDATE v3_catalog.channels SET scope='marketplace',owner_user_id=$2,settings=settings||$3::jsonb,max_concurrency=$4,max_user_concurrency=$5,multiplier_card_supported=$6 WHERE id=$1 AND (owner_user_id IS NULL OR owner_user_id=$2)`, c.catalogID, c.owner, c.settings, cmInt(c.row, "max_concurrency"), cmInt(c.row, "user_max_concurrency"), c.row.text("multiplier_card_supported") == "true")
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return errors.New("legacy: target channel owner conflict")
			}
		}
		if _, err = target.Exec(ctx, `INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES($1,$2) ON CONFLICT DO NOTHING`, c.catalogID, g.text("internal_group_name")); err != nil {
			return err
		}
		for _, model := range c.models {
			if _, err = target.Exec(ctx, `INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES($1,$2) ON CONFLICT DO NOTHING`, c.catalogID, model); err != nil {
				return err
			}
		}
	}
	for _, r := range data.records {
		if r.table == "v3_channelmarket.route_pools" {
			if _, err := target.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES($1) ON CONFLICT DO NOTHING`, r.values["internal_group_name"]); err != nil {
				return err
			}
		}
	}
	for _, r := range data.records {
		if err := cmInsert(ctx, target, r); err != nil {
			return err
		}
	}
	for _, table := range channelMarketSourceTables {
		if !cmStreamedTable(table) {
			continue
		}
		if err := data.streamBatches(ctx, table, func(records []cmRecord) error {
			values := make([]map[string]any, len(records))
			for i, record := range records {
				values[i] = record.values
			}
			return insertExactBulk(ctx, target, "v3_channelmarket", table, records[0].keys, values)
		}); err != nil {
			return err
		}
	}
	for owner, amount := range data.pending {
		if err := opening(ctx, target, "user", owner, "marketplace_pending", amount); err != nil {
			return err
		}
	}
	if data.platformRevenue != nil {
		if err := opening(ctx, target, "platform", 1, "platform_revenue", *data.platformRevenue); err != nil {
			return err
		}
	}
	if err := data.importRouting(ctx, target); err != nil {
		return err
	}
	for _, table := range []string{"v3_channelmarket.group_invites", "v3_channelmarket.multiplier_notices", "v3_channelmarket.channel_feedback", "v3_channelmarket.multiplier_trend_snapshots"} {
		if _, err := target.Exec(ctx, `SELECT setval(pg_get_serial_sequence($1,'id'),GREATEST(COALESCE((SELECT max(id) FROM `+table+`),0),1),EXISTS(SELECT 1 FROM `+table+`))`, table); err != nil {
			return err
		}
	}
	return nil
}
