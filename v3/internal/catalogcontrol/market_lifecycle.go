package catalogcontrol

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var errMarketChannelNotReady = errors.New("catalogcontrol: market channel must be verified and approved before enabling")

// Lock the market group before the catalog channel, matching owner transitions.
// Pausing preserves its identity, access grants, bookmarks and pool members.
func prepareMarketStatusTx(ctx context.Context, tx pgx.Tx, channel int64, requested string) error {
	var state, verification string
	var deleted bool
	err := tx.QueryRow(ctx, `SELECT lifecycle_status,verification_status,deleted_at IS NOT NULL
		FROM v3_channelmarket.groups WHERE channel_id=$1 FOR UPDATE`, channel).Scan(&state, &verification, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // Official channels have no market lifecycle.
	}
	if err != nil {
		return err
	}
	if requested == "enabled" {
		if deleted || (state != "active" && (state != "paused" || verification != "passed")) {
			return errMarketChannelNotReady
		}
		if state == "paused" {
			_, err = tx.Exec(ctx, `UPDATE v3_channelmarket.groups SET lifecycle_status='active',review_reason='',published_at=coalesce(published_at,now()) WHERE channel_id=$1`, channel)
		}
	} else if state == "active" {
		_, err = tx.Exec(ctx, `UPDATE v3_channelmarket.groups SET lifecycle_status='paused' WHERE channel_id=$1`, channel)
	}
	return err
}

// The request may carry the old community projection; refresh it after the save.
func syncMarketStatusTx(ctx context.Context, tx pgx.Tx, channel int64) error {
	_, err := tx.Exec(ctx, `UPDATE v3_catalog.channels c SET settings=jsonb_set(c.settings,'{community}',
		jsonb_build_object('id',g.public_channel_id,'slug',g.public_slug,'name',g.display_name,
		'visibility',g.visibility,'lifecycle_status',g.lifecycle_status,'verification_status',g.verification_status))
		FROM v3_channelmarket.groups g WHERE g.channel_id=c.id AND c.id=$1`, channel)
	return err
}
