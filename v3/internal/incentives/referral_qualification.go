package incentives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type referralOrder struct {
	id, user, plan, amount      int64
	kind, state, version, trade string
	terms                       []byte
	createdAt                   time.Time
}

func loadReferralOrder(ctx context.Context, tx pgx.Tx, id int64) (referralOrder, error) {
	var o referralOrder
	err := tx.QueryRow(ctx, `SELECT id,user_id,COALESCE(plan_id,0),amount_minor,kind,state,policy_version,trade_no,referral_terms,created_at FROM v3_commerce.orders WHERE id=$1`, id).Scan(&o.id, &o.user, &o.plan, &o.amount, &o.kind, &o.state, &o.version, &o.trade, &o.terms, &o.createdAt)
	return o, err
}
func (o referralOrder) eligible() bool {
	return o.amount > 0 && (o.kind == "topup" || (o.kind == "subscription" && o.version == "standard_v2"))
}
func referralLock(ctx context.Context, tx pgx.Tx, user int64) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("referral-first-purchase:%d", user))
	return err
}
func saveReferralTerms(ctx context.Context, tx pgx.Tx, order int64, terms any) error {
	raw, err := json.Marshal(terms)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.orders SET referral_terms=$2 WHERE id=$1 AND referral_terms='{}'::jsonb`, order, raw)
	return err
}

// ReserveReferralTx freezes eligibility and holds the complete maximum before checkout.
func (s *Service) ReserveReferralTx(ctx context.Context, tx pgx.Tx, order int64) error {
	if tx == nil || order <= 0 {
		return ErrInvalid
	}
	o, err := loadReferralOrder(ctx, tx, order)
	if err != nil {
		return err
	}
	if o.kind == "subscription" && o.version == "legacy" {
		p, e := scanReferralPolicy(tx.QueryRow(ctx, `SELECT `+referralPolicyColumns+` FROM v3_commerce.referral_consumption_policy WHERE id`))
		if e != nil {
			return e
		}
		return saveReferralTerms(ctx, tx, o.id, map[string]any{"version": "legacy", "legacy_reset_eligible": p.EffectiveAt == nil || o.createdAt.Before(*p.EffectiveAt)})
	}
	if !o.eligible() {
		return nil
	}
	if o.state != "created" {
		return ErrReferralConflict
	}
	if string(o.terms) != "{}" {
		return nil
	}
	if err = referralLock(ctx, tx, o.user); err != nil {
		return err
	}
	o, err = loadReferralOrder(ctx, tx, order)
	if err != nil {
		return err
	}
	if string(o.terms) != "{}" {
		return nil
	}
	var inviter *int64
	var previous bool
	err = tx.QueryRow(ctx, `SELECT inviter_id,EXISTS(SELECT 1 FROM v3_commerce.orders WHERE user_id=$1 AND id<>$2 AND state IN('paid','refunded') AND amount_minor>0 AND (kind='topup' OR (kind='subscription' AND policy_version='standard_v2')))
 OR EXISTS(SELECT 1 FROM v3_commerce.referral_purchase_rewards WHERE invitee_id=$1)
 OR EXISTS(SELECT 1 FROM v3_commerce.referral_consumption_qualifications WHERE invitee_id=$1 AND state IN('reserved','active','completed'))
 FROM v3_identity.users WHERE id=$1`, o.user, o.id).Scan(&inviter, &previous)
	if err != nil {
		return err
	}
	p, err := scanReferralPolicy(tx.QueryRow(ctx, `SELECT `+referralPolicyColumns+` FROM v3_commerce.referral_consumption_policy WHERE id FOR UPDATE`))
	if err != nil {
		return err
	}
	reason := ""
	switch {
	case inviter == nil || *inviter == o.user:
		reason = "no_eligible_inviter"
	case previous:
		reason = "first_purchase_already_claimed"
	case !p.Enabled:
		reason = "program_disabled"
	case p.EffectiveAt != nil && o.createdAt.Before(*p.EffectiveAt):
		reason = "program_not_effective"
	case p.TotalBudgetCredits-p.SpentCredits-p.ReservedCredits < p.MaxRewardCredits:
		reason = "budget_unavailable"
	}
	terms := referralPublicTerms(p, reason)
	if err = saveReferralTerms(ctx, tx, o.id, terms); err != nil || reason != "" {
		return err
	}
	raw, err := json.Marshal(terms)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.referral_consumption_qualifications(order_id,inviter_id,invitee_id,state,terms,reward_ppm,profit_share_ppm,window_days,delay_days,max_reward_credits,reserved_credits,created_at,updated_at,ancillary_cost_ppm)
 VALUES($1,$2,$3,'reserved',$4,$5,$6,$7,$8,$9,$9,$10,$10,$11)`, o.id, *inviter, o.user, raw, p.RewardPPM, p.ProfitSharePPM, p.WindowDays, p.DelayDays, p.MaxRewardCredits, s.now(), p.AncillaryCostPPM)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_policy SET reserved_credits=reserved_credits+$1,updated_at=$2 WHERE id`, p.MaxRewardCredits, s.now())
	return err
}

// ReleaseReferralTx keeps frozen terms for a late payment but releases unused budget.
func (s *Service) ReleaseReferralTx(ctx context.Context, tx pgx.Tx, order int64) error {
	if tx == nil || order <= 0 {
		return ErrInvalid
	}
	o, err := loadReferralOrder(ctx, tx, order)
	if err != nil {
		return err
	}
	if err = referralLock(ctx, tx, o.user); err != nil {
		return err
	}
	var reserved int64
	err = tx.QueryRow(ctx, `UPDATE v3_commerce.referral_consumption_qualifications SET state='released',reason='checkout_released',reserved_credits=0,updated_at=$2 WHERE order_id=$1 AND state='reserved' RETURNING max_reward_credits`, order, s.now()).Scan(&reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_policy SET reserved_credits=reserved_credits-$1,updated_at=$2 WHERE id`, reserved, s.now())
	return err
}

// PurchaseTx validates immutable payment facts before dispatching old/new promises.
func (s *Service) PurchaseTx(ctx context.Context, tx pgx.Tx, p Purchase) error {
	if tx == nil || p.OrderID <= 0 || p.UserID <= 0 || p.SourceID == "" || len(p.SourceID) > 200 {
		return ErrInvalid
	}
	o, err := loadReferralOrder(ctx, tx, p.OrderID)
	if err != nil {
		return err
	}
	expected := "subscription_order"
	if o.kind == "topup" {
		expected = "topup_order"
	}
	if o.user != p.UserID || o.plan != p.PlanID || o.amount != p.AmountMinor || o.trade != p.SourceID || p.SourceType != expected || o.state != "paid" {
		return ErrInvalid
	}
	if o.kind == "subscription" && o.version == "legacy" {
		if err = referralLock(ctx, tx, o.user); err != nil {
			return err
		}
		allowed, e := s.legacyResetAllowed(ctx, tx, o)
		if e != nil {
			return e
		}
		if !allowed {
			return nil
		}
		return s.legacyPurchaseTx(ctx, tx, p)
	}
	if !o.eligible() {
		return nil
	}
	if err = referralLock(ctx, tx, o.user); err != nil {
		return err
	}
	var state string
	err = tx.QueryRow(ctx, `SELECT state FROM v3_commerce.referral_consumption_qualifications WHERE order_id=$1 FOR UPDATE`, o.id).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.unreservedReferralTx(ctx, tx, o)
	}
	if err != nil {
		return err
	}
	if state == "active" || state == "completed" || state == "refunded" {
		return nil
	}
	if state != "reserved" {
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_qualifications SET state='needs_review',reason='paid_after_budget_release',paid_at=$2,window_until=$2::timestamptz+window_days*interval '1 day',updated_at=$2 WHERE order_id=$1`, o.id, s.now())
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.referral_purchase_rewards(inviter_id,invitee_id,purchase_type,purchase_label,bonus_credits,order_source_type,order_source_id,rewarded_at,created_at,updated_at)
 SELECT inviter_id,invitee_id,'consumption_v1','首购消费奖励资格',0,$2,$3,$4,$4,$4 FROM v3_commerce.referral_consumption_qualifications WHERE order_id=$1 ON CONFLICT(invitee_id) DO NOTHING`, o.id, expected, o.trade, s.now())
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT order_source_id=$2 FROM v3_commerce.referral_purchase_rewards WHERE invitee_id=$1`, o.user, o.trade).Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return ErrReferralConflict
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_qualifications SET state='active',paid_at=$2,window_until=$2::timestamptz+window_days*interval '1 day',updated_at=$2 WHERE order_id=$1`, o.id, s.now())
	return err
}

func (s *Service) unreservedReferralTx(ctx context.Context, tx pgx.Tx, o referralOrder) error {
	if string(o.terms) != "{}" {
		return nil
	} // Eligibility was explicitly displayed as unavailable before payment.
	p, err := scanReferralPolicy(tx.QueryRow(ctx, `SELECT `+referralPolicyColumns+` FROM v3_commerce.referral_consumption_policy WHERE id`))
	if err != nil {
		return err
	}
	if !p.Enabled {
		return nil
	}
	var inviter *int64
	if err = tx.QueryRow(ctx, `SELECT inviter_id FROM v3_identity.users WHERE id=$1`, o.user).Scan(&inviter); err != nil {
		return err
	}
	if inviter == nil || *inviter == o.user {
		return nil
	}
	raw, err := json.Marshal(referralPublicTerms(p, "missing_prepaid_reservation"))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.referral_consumption_qualifications(order_id,inviter_id,invitee_id,state,reason,terms,reward_ppm,profit_share_ppm,window_days,delay_days,max_reward_credits,reserved_credits,paid_at,window_until,created_at,updated_at,ancillary_cost_ppm)
 VALUES($1,$2,$3,'needs_review','missing_prepaid_reservation',$4,$5,$6,$7,$8,$9,0,$10,$10::timestamptz+$7::integer*interval '1 day',$10,$10,$11) ON CONFLICT(order_id) DO NOTHING`, o.id, *inviter, o.user, raw, p.RewardPPM, p.ProfitSharePPM, p.WindowDays, p.DelayDays, p.MaxRewardCredits, s.now(), p.AncillaryCostPPM)
	return err
}
