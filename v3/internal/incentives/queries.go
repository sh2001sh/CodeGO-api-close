package incentives

import (
	"bytes"
	"context"
	"encoding/json"
)

func decodeValue(raw []byte) (any, error) {
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	err := d.Decode(&value)
	return value, err
}
func listJSON(ctx context.Context, q querier, query string, args ...any) ([]any, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []any{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		v, err := decodeValue(raw)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

const adminDrawView = `to_jsonb(d)||jsonb_build_object('drawn_at',COALESCE(EXTRACT(epoch FROM d.drawn_at)::bigint,0),'completed_at',COALESCE(EXTRACT(epoch FROM d.completed_at)::bigint,0))`
const rewardView = `jsonb_build_object('reward',jsonb_build_object('id',r.id,'draw_id',r.draw_id,'user_subscription_id',COALESCE(r.subscription_id,0),'blind_box_open_record_id',COALESCE(r.open_record_id,0),'participation_type',r.participation_type,'lucky_number',r.lucky_number,'membership_tier',r.membership_tier,'matched_digits',r.matched_digits,'base_reward_usd',r.base_reward_usd,'tier_multiplier',r.tier_multiplier,'jackpot_reward_usd',r.jackpot_reward_usd,'final_reward_quota',r.final_reward_credits,'credit_status',r.credit_status,'credited_at',COALESCE(EXTRACT(epoch FROM r.credited_at)::bigint,0)),
 'draw_date',d.draw_date,'winning_number',d.winning_number,'reward_usd',r.final_reward_credits::numeric/1000000)`

// Self no longer exposes current participation after retirement. History and
// notifications remain owner-scoped so already determined rewards stay visible.
func (s *Service) Self(context.Context, int64) (map[string]any, error) {
	return nil, ErrRetired
}
func pageBounds(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		page = 1000000
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}
func (s *Service) History(ctx context.Context, user int64, page, size int) (map[string]any, error) {
	page, size = pageBounds(page, size)
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_lucky_rewards WHERE user_id=$1 AND matched_digits>0`, user).Scan(&total); err != nil {
		return nil, err
	}
	records, err := listJSON(ctx, s.pool, `SELECT `+rewardView+` FROM v3_commerce.subscription_lucky_rewards r JOIN v3_commerce.subscription_lucky_draws d ON d.id=r.draw_id WHERE user_id=$1 AND matched_digits>0 ORDER BY r.id DESC LIMIT $2 OFFSET $3`, user, size, (page-1)*size)
	return map[string]any{"page": page, "page_size": size, "total": total, "records": records}, err
}
func (s *Service) PublicWins(ctx context.Context, date string, page, size int) (map[string]any, error) {
	page, size = pageBounds(page, size)
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_lucky_rewards r JOIN v3_commerce.subscription_lucky_draws d ON d.id=r.draw_id WHERE matched_digits>0 AND credit_status='credited' AND ($1='' OR d.draw_date=$1)`, date).Scan(&total); err != nil {
		return nil, err
	}
	records, err := listJSON(ctx, s.pool, `SELECT jsonb_build_object('draw_date',d.draw_date,'winning_number',d.winning_number,'membership_tier',r.membership_tier,'lucky_suffix','**'||right(r.lucky_number,2),'matched_digits',r.matched_digits,'reward_usd',r.final_reward_credits::numeric/1000000)
 FROM v3_commerce.subscription_lucky_rewards r JOIN v3_commerce.subscription_lucky_draws d ON d.id=r.draw_id WHERE matched_digits>0 AND credit_status='credited' AND ($1='' OR d.draw_date=$1) ORDER BY r.id DESC LIMIT $2 OFFSET $3`, date, size, (page-1)*size)
	return map[string]any{"page": page, "page_size": size, "total": total, "records": records}, err
}
func (s *Service) Notifications(ctx context.Context, user int64) (map[string]any, error) {
	var count int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_lucky_reward_notifications WHERE user_id=$1 AND read_at IS NULL`, user).Scan(&count); err != nil {
		return nil, err
	}
	items, err := listJSON(ctx, s.pool, `SELECT jsonb_build_object('id',n.id,'reward',`+rewardView+`,'read_at',COALESCE(EXTRACT(epoch FROM n.read_at)::bigint,0),'created_at',EXTRACT(epoch FROM n.created_at)::bigint)
 FROM v3_commerce.subscription_lucky_reward_notifications n JOIN v3_commerce.subscription_lucky_rewards r ON r.id=n.reward_id JOIN v3_commerce.subscription_lucky_draws d ON d.id=r.draw_id WHERE n.user_id=$1 ORDER BY n.id DESC LIMIT 10`, user)
	return map[string]any{"unread_count": count, "items": items}, err
}
func (s *Service) MarkRead(ctx context.Context, user, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE v3_commerce.subscription_lucky_reward_notifications SET read_at=COALESCE(read_at,$3),updated_at=$3 WHERE user_id=$1 AND ($2::bigint=0 OR id=$2)`, user, id, s.now())
	if err == nil && id != 0 && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
func (s *Service) AffiliateRewards(ctx context.Context, user int64) (map[string]any, error) {
	code, err := s.AffiliateCode(ctx, user)
	if err != nil {
		return nil, err
	}
	sum, err := s.ResetOpportunities(ctx, user)
	if err != nil {
		return nil, err
	}
	invitees, err := listJSON(ctx, s.pool, `SELECT jsonb_build_object('invitee_id',u.id,'invitee_external_id',u.external_id,'invitee_username',u.username,'invitee_display_name',u.display_name,'created_at',EXTRACT(epoch FROM u.created_at)::bigint,
 'month_card_purchased',r.id IS NOT NULL,'reset_opportunity_earned',r.id IS NOT NULL,'reset_opportunity_earned_at',COALESCE(EXTRACT(epoch FROM r.created_at)::bigint,0))
 FROM v3_identity.users u LEFT JOIN v3_commerce.subscription_reset_opportunity_ledgers r ON r.user_id=$1 AND r.related_user_id=u.id AND r.change_type='earn' WHERE u.inviter_id=$1 ORDER BY u.created_at DESC,u.id DESC`, user)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, v := range invitees {
		if v.(map[string]any)["reset_opportunity_earned"] == true {
			count++
		}
	}
	return map[string]any{"affiliate_code": code, "invited_count": len(invitees), "successful_purchase_invites": count, "reset_opportunity": sum, "invitees": invitees}, nil
}
