package incentives

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

func (s *Service) ReferralRewards(ctx context.Context, user int64) (ReferralRewardsSummary, error) {
	result := ReferralRewardsSummary{Records: []ReferralRewardRecord{}, OwnerOnly: true}
	if user <= 0 {
		return result, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT id,order_id,invitee_id,state,reason,terms,max_reward_credits,paid_credits,refunded_reward_credits,reserved_credits,paid_at,window_until FROM v3_commerce.referral_consumption_qualifications WHERE inviter_id=$1 ORDER BY id DESC LIMIT 100`, user)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var r ReferralRewardRecord
		var raw []byte
		if err = rows.Scan(&r.ID, &r.OrderID, &r.InviteeID, &r.State, &r.Reason, &raw, &r.MaxRewardCredits, &r.PaidCredits, &r.RefundedRewardCredits, &r.ReservedCredits, &r.PaidAt, &r.WindowUntil); err != nil {
			rows.Close()
			return result, err
		}
		if err = json.Unmarshal(raw, &r.Terms); err != nil {
			rows.Close()
			return result, err
		}
		result.Records = append(result.Records, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT debt_credits FROM v3_commerce.referral_consumption_offsets WHERE user_id=$1),0)`, user).Scan(&result.RefundOffsetCredits); err != nil {
		return result, err
	}
	p, err := s.ReferralPolicy(ctx)
	if err != nil {
		return result, err
	}
	result.NewInvitesGrantRefresh = p.EffectiveAt == nil || s.now().Before(*p.EffectiveAt)
	return result, nil
}
func (s *Service) ReferralQualifications(ctx context.Context, page, size int) (ReferralQualificationsResult, error) {
	page, size = pageBounds(page, size)
	result := ReferralQualificationsResult{Records: []ReferralQualificationRecord{}, Page: page, PageSize: size}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.referral_consumption_qualifications`).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := s.pool.Query(ctx, `SELECT to_jsonb(q) FROM v3_commerce.referral_consumption_qualifications q ORDER BY id DESC LIMIT $1 OFFSET $2`, size, (page-1)*size)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var record ReferralQualificationRecord
		if err = rows.Scan(&raw); err != nil {
			return result, err
		}
		if err = json.Unmarshal(raw, &record); err != nil {
			return result, err
		}
		result.Records = append(result.Records, record)
	}
	return result, rows.Err()
}

// ApproveReferralReview re-reserves budget for a paid qualification whose
// original checkout reservation was missing/released. It cannot bypass costs.
func (s *Service) ApproveReferralReview(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var user, order int64
		if err := tx.QueryRow(ctx, `SELECT invitee_id,order_id FROM v3_commerce.referral_consumption_qualifications WHERE id=$1`, id).Scan(&user, &order); err != nil {
			return err
		}
		var orderPaid bool
		if err := tx.QueryRow(ctx, `SELECT state='paid' FROM v3_commerce.orders WHERE id=$1 FOR SHARE`, order).Scan(&orderPaid); err != nil {
			return err
		}
		if !orderPaid {
			return ErrReferralConflict
		}
		if err := referralLock(ctx, tx, user); err != nil {
			return err
		}
		q, err := scanQualification(tx.QueryRow(ctx, `SELECT `+qualificationColumns+` FROM v3_commerce.referral_consumption_qualifications WHERE id=$1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if q.state == "active" || q.state == "completed" {
			return nil
		}
		if q.state != "needs_review" || (q.reason != "paid_after_budget_release" && q.reason != "missing_prepaid_reservation" && q.reason != "refund_reinstatement_budget") {
			return ErrReferralConflict
		}
		var paid, claimed bool
		var trade, kind string
		err = tx.QueryRow(ctx, `SELECT o.state='paid',o.trade_no,o.kind,EXISTS(SELECT 1 FROM v3_commerce.referral_purchase_rewards r WHERE r.invitee_id=o.user_id AND r.order_source_id<>o.trade_no)
  OR EXISTS(SELECT 1 FROM v3_commerce.referral_consumption_qualifications other WHERE other.invitee_id=o.user_id AND other.id<>$2 AND other.state IN('reserved','active','completed'))
  OR EXISTS(SELECT 1 FROM v3_commerce.orders previous WHERE previous.user_id=o.user_id AND previous.id<>o.id AND previous.state IN('paid','refunded') AND previous.amount_minor>0
  AND (previous.kind='topup' OR (previous.kind='subscription' AND previous.policy_version='standard_v2'))
  AND (previous.paid_at IS NULL OR previous.paid_at<o.paid_at OR (previous.paid_at=o.paid_at AND previous.id<o.id))) FROM v3_commerce.orders o WHERE o.id=$1`, q.order, q.id).Scan(&paid, &trade, &kind, &claimed)
		if err != nil {
			return err
		}
		if !paid || claimed {
			return ErrReferralConflict
		}
		p, err := scanReferralPolicy(tx.QueryRow(ctx, `SELECT `+referralPolicyColumns+` FROM v3_commerce.referral_consumption_policy WHERE id FOR UPDATE`))
		if err != nil {
			return err
		}
		needed := q.max - (q.paid - q.reversed) - q.reserved
		if int64(p.TotalBudgetCredits-p.SpentCredits-p.ReservedCredits) < needed {
			return ErrReferralConflict
		}
		source := "subscription_order"
		if kind == "topup" {
			source = "topup_order"
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.referral_purchase_rewards(inviter_id,invitee_id,purchase_type,purchase_label,bonus_credits,order_source_type,order_source_id,rewarded_at,created_at,updated_at)
  VALUES($1,$2,'consumption_v1','首购消费奖励资格',$3,$4,$5,$6,$6,$6) ON CONFLICT(invitee_id) DO NOTHING`, q.inviter, q.invitee, 0, source, trade, s.now()); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_policy SET reserved_credits=reserved_credits+$1 WHERE id`, needed); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_qualifications SET state='active',reason='',reserved_credits=reserved_credits+$2,updated_at=$3 WHERE id=$1`, q.id, needed, s.now())
		return err
	})
}
