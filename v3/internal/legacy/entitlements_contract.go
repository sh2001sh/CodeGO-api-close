package legacy

// Entitlement columns are native typed state and historical facts. Credits are
// doubled exactly; numeric USD and multipliers retain their source precision.
type entitlementContract struct {
	source string
	spec   string
}

var entitlementContracts = []entitlementContract{
	{"subscription_lucky_numbers", `id:id:i subscription_id:user_subscription_id:i user_id:user_id:i card_code:card_code:s lucky_suffix:lucky_suffix:s assigned_at:assigned_at:t created_at:created_at:t updated_at:updated_at:t`},
	{"blind_box_daily_lucky_numbers", `id:id:i open_record_id:blind_box_open_record_id:i user_id:user_id:i draw_date:draw_date:s lucky_suffix:lucky_suffix:s expires_at:expires_at:t created_at:created_at:t`},
	{"subscription_lucky_draws", `id:id:i draw_date:draw_date:s winning_number:winning_number:s jackpot_before:jackpot_before:n jackpot_after:jackpot_after:n full_match_count:full_match_count:i status:status:s error_message:error_message:s timezone:timezone:s draw_hour:draw_hour:i draw_minute:draw_minute:i
		base_reward_1_usd:base_reward1_usd:n base_reward_2_usd:base_reward2_usd:n base_reward_3_usd:base_reward3_usd:n base_reward_4_usd:base_reward4_usd:n multiplier_lite:multiplier_lite:n multiplier_standard:multiplier_standard:n multiplier_pro:multiplier_pro:n multiplier_ultra:multiplier_ultra:n
		jackpot_initial_usd:jackpot_initial_usd:n jackpot_increment_usd:jackpot_increment_usd:n jackpot_cap_usd:jackpot_cap_usd:n cost_per_usd:cost_per_usd:n monthly_budget_usd:monthly_budget_usd:n drawn_at:drawn_at:T completed_at:completed_at:T created_at:created_at:t updated_at:updated_at:t`},
	{"subscription_lucky_rewards", `id:id:i draw_id:draw_id:i subscription_id:user_subscription_id:i open_record_id:blind_box_open_record_id:i participation_type:participation_type:s user_id:user_id:i lucky_number:lucky_number:s membership_tier:membership_tier:s matched_digits:matched_digits:i base_reward_usd:base_reward_usd:n tier_multiplier:tier_multiplier:n jackpot_reward_usd:jackpot_reward_usd:n final_reward_credits:final_reward_quota:c credit_status:credit_status:s credit_error:credit_error:s credited_at:credited_at:T created_at:created_at:t updated_at:updated_at:t`},
	{"subscription_lucky_reward_notifications", `id:id:i reward_id:reward_id:i user_id:user_id:i read_at:read_at:T created_at:created_at:t updated_at:updated_at:t`},
	{"subscription_blind_box_benefit_cycles", `id:id:i subscription_id:user_subscription_id:i benefit_cycle:benefit_cycle:s user_id:user_id:i membership_tier:membership_tier:s expected_count:expected_count:i granted_count:granted_count:i source:source:s idempotency_key:idempotency_key:s starts_at:starts_at:t ends_at:ends_at:t status:status:s created_at:created_at:t updated_at:updated_at:t`},
	{"subscription_reset_opportunity_accounts", `id:id:i user_id:user_id:i earned_total:earned_total:i used_total:used_total:i available_total:available_total:i last_used_month:last_used_month:s created_at:created_at:t updated_at:updated_at:t`},
	{"subscription_reset_opportunity_ledgers", `id:id:i user_id:user_id:i related_user_id:related_user_id:i change_type:change_type:s delta:delta:i balance_after:balance_after:i used_month:used_month:s source_type:source_type:s source_ref:source_ref:s event_key:event_key:s note:note:s created_at:created_at:t updated_at:updated_at:t`},
	{"referral_purchase_rewards", `id:id:i inviter_id:inviter_id:i invitee_id:invitee_id:i purchase_type:purchase_type:s purchase_label:purchase_label:s bonus_credits:bonus_quota_amount:c order_source_type:order_source_type:s order_source_id:order_source_id:s rewarded_at:rewarded_at:t created_at:created_at:t updated_at:updated_at:t`},
	{"subscription_claude_conversions", `id:id:i user_id:user_id:i subscription_id:user_subscription_id:i request_id:request_id:s status:status:s source_credits:source_quota:c target_credits:target_claude_quota:c plan_price_amount:plan_price_amount:n unused_ratio:unused_ratio:n conversion_percent:conversion_percent:i ratio_numerator:ratio_numerator:i ratio_denominator:ratio_denominator:i created_at:created_at:t updated_at:updated_at:t`},
}
