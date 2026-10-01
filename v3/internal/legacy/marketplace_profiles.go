package legacy

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Rebuild the first-request profile from imported active cards, including their
// budget state. The comparison time is only a query filter; source facts remain
// unchanged and importing a historical expired active row grants nothing.
func importMarketplaceProfiles(ctx context.Context, target pgx.Tx) error {
	_, err := target.Exec(ctx, `INSERT INTO v3_marketplace.account_profiles(user_id,multiplier_ppm,expires_at,cards,updated_at)
	SELECT user_id,(array_agg(multiplier_ppm ORDER BY multiplier_ppm,expires_at DESC))[1],
	(array_agg(expires_at ORDER BY multiplier_ppm,expires_at DESC))[1],
	jsonb_agg(jsonb_build_object('id',id,'multiplier_ppm',multiplier_ppm,'expires_at',expires_at,'prop_type',prop_type,'max_discount_micro',max_discount_micro,'used_discount_micro',used_discount_micro) ORDER BY multiplier_ppm,expires_at DESC),max(updated_at)
	FROM v3_marketplace.blind_box_props WHERE kind='multiplier' AND status='active' AND expires_at>now()
	AND (max_discount_micro=0 OR used_discount_micro<max_discount_micro OR prop_type IN('consume_discount_95','consume_discount_90','consume_discount_10','zero_hour_multiplier','monthly_pass_multiplier')) GROUP BY user_id
	ON CONFLICT(user_id) DO UPDATE SET multiplier_ppm=EXCLUDED.multiplier_ppm,expires_at=EXCLUDED.expires_at,cards=EXCLUDED.cards,updated_at=EXCLUDED.updated_at`)
	return err
}
