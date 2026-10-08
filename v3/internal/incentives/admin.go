package incentives

import "context"

func (s *Service) AdminDraws(ctx context.Context, page, size int) (map[string]any, error) {
	page, size = pageBounds(page, size)
	c, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	var total int64
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_lucky_draws`).Scan(&total); err != nil {
		return nil, err
	}
	draws, err := listJSON(ctx, s.pool, `SELECT jsonb_build_object('draw',`+adminDrawView+`,'participant_count',stats.participants,'reward_count',stats.participants,'credited_count',stats.credited,'nominal_reward_usd',stats.nominal,'actual_cost_cny',stats.nominal*d.cost_per_usd)
 FROM v3_commerce.subscription_lucky_draws d LEFT JOIN LATERAL(SELECT count(*) participants,count(*) FILTER(WHERE credit_status='credited') credited,COALESCE(sum(base_reward_usd*tier_multiplier+jackpot_reward_usd),0) nominal FROM v3_commerce.subscription_lucky_rewards WHERE draw_id=d.id) stats ON true ORDER BY draw_date DESC,d.id DESC LIMIT $1 OFFSET $2`, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	loc, err := loadLocation(c.Timezone)
	if err != nil {
		return nil, err
	}
	stats, err := listJSON(ctx, s.pool, `SELECT jsonb_build_object('monthly_nominal_reward_usd',COALESCE(sum(r.base_reward_usd*r.tier_multiplier+r.jackpot_reward_usd),0),'monthly_actual_cost_cny',COALESCE(sum((r.base_reward_usd*r.tier_multiplier+r.jackpot_reward_usd)*d.cost_per_usd),0),'monthly_budget_usage_percent',CASE WHEN $2::numeric>0 THEN COALESCE(sum(r.base_reward_usd*r.tier_multiplier+r.jackpot_reward_usd),0)/$2::numeric*100 ELSE 0 END)
 FROM v3_commerce.subscription_lucky_rewards r JOIN v3_commerce.subscription_lucky_draws d ON d.id=r.draw_id WHERE draw_date LIKE $1`, s.now().In(loc).Format("2006-01")+"-%", string(c.Budget))
	if err != nil {
		return nil, err
	}
	result := stats[0].(map[string]any)
	result["config"] = c
	result["draws"] = draws
	result["page"] = page
	result["page_size"] = size
	result["total"] = total
	result["monthly_budget_usd"] = c.Budget
	return result, nil
}
