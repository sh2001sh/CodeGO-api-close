package catalogcontrol

import (
	"context"

	"github.com/jackc/pgx/v5"
)

const visibleCatalogChannel = `NOT EXISTS(SELECT 1 FROM v3_channelmarket.groups deleted_group WHERE deleted_group.channel_id=c.id AND deleted_group.deleted_at IS NOT NULL)`

// Market channels own financial and verification history. Retire their routing
// identity instead of hard-deleting a row referenced by those records.
func (s *Server) deleteChannels(ctx context.Context, ids []int64, disabledOnly bool) (int64, error) {
	var count int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT g.channel_id FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id WHERE ($1::bigint[] IS NULL OR c.id=ANY($1)) AND (NOT $2 OR c.status IN ('disabled','auto_disabled')) AND g.deleted_at IS NULL ORDER BY g.channel_id FOR UPDATE OF g`, ids, disabledOnly)
		if err != nil {
			return err
		}
		var market []int64
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			market = append(market, id)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		if len(market) > 0 {
			if _, err = tx.Exec(ctx, `UPDATE v3_channelmarket.groups SET lifecycle_status='deleted',deleted_at=now() WHERE channel_id=ANY($1::bigint[])`, market); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE v3_catalog.channels c SET status='disabled',settings=jsonb_set(jsonb_set(c.settings,'{community}',coalesce(c.settings->'community','{}')||'{"lifecycle_status":"deleted"}'::jsonb),'{market}',coalesce(c.settings->'market','{}')||'{"auto_probe_enabled":false}'::jsonb) WHERE c.id=ANY($1::bigint[])`, market); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE v3_catalog.channel_credentials SET status='disabled' WHERE channel_id=ANY($1::bigint[])`, market); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE v3_channelmarket.verification_runs SET status='paused',completed_at=now(),summary='Channel deleted' WHERE channel_id=ANY($1::bigint[]) AND status IN ('queued','running')`, market); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `DELETE FROM v3_catalog.channels c WHERE ($1::bigint[] IS NULL OR c.id=ANY($1)) AND (NOT $2 OR c.status IN ('disabled','auto_disabled')) AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.groups g WHERE g.channel_id=c.id)`, ids, disabledOnly)
		if err == nil {
			count = int64(len(market)) + tag.RowsAffected()
		}
		return err
	})
	return count, err
}
