package channelmarket

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// The public marketplace ID is independent of the catalog's channel sequence:
// imported legacy channels may already own a catalog number. Serializing this
// allocation prevents concurrent creates from claiming the same public ID,
// while reading retained rows and community projections reserves historical IDs.
func nextPublicChannelID(ctx context.Context, tx pgx.Tx) (string, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('v3_channelmarket_public_channel_id'))`); err != nil {
		return "", err
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT (coalesce(max(CASE WHEN public_id ~ '^[0-9]+$' THEN public_id::numeric END),0)+1)::text FROM (
		SELECT public_channel_id AS public_id FROM v3_channelmarket.groups
		UNION ALL
		SELECT settings->'community'->>'id' FROM v3_catalog.channels WHERE scope='marketplace'
	) occupied`).Scan(&id)
	return id, err
}
