package incentives

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// legacyPurchaseTx preserves the first-monthly-purchase promise frozen by old orders.
func (s *Service) legacyPurchaseTx(ctx context.Context, tx pgx.Tx, p Purchase) error {
	if tx == nil || p.UserID <= 0 || p.OrderID <= 0 || p.PlanID <= 0 || p.SourceID == "" || len(p.SourceID) > 200 || p.SourceType != "subscription_order" {
		return ErrInvalid
	}
	if p.AmountMinor <= 0 {
		return nil
	}
	var inviter *int64
	var monthly, paid bool
	var source string
	var amount, user, plan int64
	err := tx.QueryRow(ctx, `SELECT u.inviter_id,COALESCE(NULLIF(o.plan_snapshot->>'plan_type',''),p.plan_type)='monthly',o.state='paid' AND o.amount_minor>0 AND o.user_id=$2 AND o.plan_id=$3 AND o.purchase_type<>'fuel' AND o.policy_version='legacy'
 ,o.trade_no,o.amount_minor,o.user_id,o.plan_id FROM v3_commerce.orders o JOIN v3_identity.users u ON u.id=o.user_id JOIN v3_commerce.plans p ON p.id=o.plan_id WHERE o.id=$1`, p.OrderID, p.UserID, p.PlanID).Scan(&inviter, &monthly, &paid, &source, &amount, &user, &plan)
	if err != nil {
		return err
	}
	if source != p.SourceID || amount != p.AmountMinor || user != p.UserID || plan != p.PlanID {
		return ErrInvalid
	}
	if !monthly || !paid {
		return nil
	}
	if inviter == nil || *inviter == p.UserID {
		return nil
	}
	key := fmt.Sprintf("referral-reset-opportunity:%d", p.UserID)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscription_reset_opportunity_ledgers WHERE event_key=$1)
 OR EXISTS(SELECT 1 FROM v3_commerce.referral_purchase_rewards WHERE invitee_id=$2 AND purchase_type='consumption_v1')
 OR EXISTS(SELECT 1 FROM v3_commerce.orders o JOIN v3_commerce.plans p ON p.id=o.plan_id WHERE o.user_id=$2 AND o.id<>$3 AND o.state='paid' AND o.amount_minor>0 AND COALESCE(NULLIF(o.plan_snapshot->>'plan_type',''),p.plan_type)='monthly')`, key, p.UserID, p.OrderID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, *inviter); err != nil {
		return err
	}
	var available, earned int64
	if err = tx.QueryRow(ctx, `SELECT available_total,earned_total FROM v3_commerce.subscription_reset_opportunity_accounts WHERE user_id=$1 FOR UPDATE`, *inviter).Scan(&available, &earned); err != nil {
		return err
	}
	if available == math.MaxInt64 || earned == math.MaxInt64 {
		return credits.ErrOverflow
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_reset_opportunity_accounts SET available_total=available_total+1,earned_total=earned_total+1,updated_at=$2 WHERE user_id=$1`, *inviter, s.now()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_ledgers(user_id,related_user_id,change_type,delta,balance_after,source_type,source_ref,event_key,note,created_at,updated_at)
 VALUES($1,$2,'earn',1,$3,$4,$5,$6,'邀请新用户首购月卡赠送额度重置机会',$7,$7)`, *inviter, p.UserID, available+1, p.SourceType, p.SourceID, key, s.now()); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.referral_purchase_rewards(inviter_id,invitee_id,purchase_type,purchase_label,bonus_credits,order_source_type,order_source_id,rewarded_at,created_at,updated_at)
 VALUES($1,$2,'month_card','月卡首购重置机会',0,$3,$4,$5,$5,$5) ON CONFLICT(invitee_id) DO NOTHING`, *inviter, p.UserID, p.SourceType, p.SourceID, s.now())
	return err
}
func summary(ctx context.Context, q rowQuerier, user int64, month string) (ResetSummary, error) {
	result := ResetSummary{CurrentMonth: month}
	err := q.QueryRow(ctx, `SELECT available_total,earned_total,used_total,exchanged_total,last_used_month FROM v3_commerce.subscription_reset_opportunity_accounts WHERE user_id=$1`, user).Scan(&result.AvailableCount, &result.EarnedTotal, &result.UsedTotal, &result.ExchangedTotal, &result.LastUsedMonth)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	result.UsedThisMonth = result.LastUsedMonth == month
	return result, err
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) ResetOpportunities(ctx context.Context, user int64) (ResetSummary, error) {
	loc, err := shanghai()
	if err != nil {
		return ResetSummary{}, err
	}
	return summary(ctx, s.pool, user, s.now().In(loc).Format("2006-01"))
}

type ResetResult struct {
	ResetOpportunity ResetSummary `json:"reset_opportunity"`
	SubscriptionID   int64        `json:"user_subscription_id"`
	UsedBefore       int64        `json:"amount_used_before"`
	UsedAfter        int64        `json:"amount_used_after"`
	PeriodUsedBefore int64        `json:"period_used_before"`
	PeriodUsedAfter  int64        `json:"period_used_after"`
	ClearedUsed      int64        `json:"cleared_used_amount"`
}

func (s *Service) UseReset(ctx context.Context, user int64) (ResetResult, error) {
	var result ResetResult
	if user <= 0 {
		return result, ErrInvalid
	}
	if s.cfg.ResetSubscriptionTx == nil {
		return result, ErrUnavailable
	}
	loc, err := shanghai()
	if err != nil {
		return result, err
	}
	month := s.now().In(loc).Format("2006-01")
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var locked int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, user).Scan(&locked); err != nil {
			return err
		}
		var available, used int64
		var last string
		err := tx.QueryRow(ctx, `SELECT available_total,used_total,last_used_month FROM v3_commerce.subscription_reset_opportunity_accounts WHERE user_id=$1 FOR UPDATE`, user).Scan(&available, &used, &last)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		if err != nil {
			return err
		}
		if available <= 0 {
			return ErrUnavailable
		}
		if last == month {
			return ErrMonthlyUsed
		}
		if used == math.MaxInt64 {
			return credits.ErrOverflow
		}
		err = tx.QueryRow(ctx, `SELECT s.id,s.used_credits+COALESCE(u.spent,0),s.period_used+COALESCE(u.spent,0) FROM v3_commerce.subscriptions s JOIN v3_identity.users owner ON owner.id=s.user_id
   LEFT JOIN LATERAL(SELECT GREATEST(-SUM(amount),0)::bigint spent FROM v3_billing.ledger_entries WHERE account_id=s.account_id AND kind IN('usage','refund')) u ON true
   WHERE s.user_id=$1 AND s.state='active' AND s.policy_version='legacy' AND s.deleted_at IS NULL AND s.starts_at<=$2 AND s.expires_at>$2
   AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_conversions c WHERE c.subscription_id=s.id AND c.cycle_order_id=COALESCE(s.order_id,0))
   AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_value_conversions c WHERE c.subscription_id=s.id AND c.status='completed' AND c.created_at>=s.starts_at)
   ORDER BY COALESCE((SELECT ord FROM jsonb_array_elements_text(COALESCE(owner.settings->'subscription_order_ids','[]'::jsonb)) WITH ORDINALITY AS wanted(id,ord) WHERE wanted.id=s.id::text),9223372036854775807),s.expires_at,s.id LIMIT 1 FOR UPDATE OF s`, user, s.now()).Scan(&result.SubscriptionID, &result.UsedBefore, &result.PeriodUsedBefore)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		key := fmt.Sprintf("use-reset-opportunity:%d:%s", user, month)
		if err = s.cfg.ResetSubscriptionTx(ctx, tx, result.SubscriptionID, key); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT used_credits,period_used FROM v3_commerce.subscriptions WHERE id=$1`, result.SubscriptionID).Scan(&result.UsedAfter, &result.PeriodUsedAfter); err != nil {
			return err
		}
		result.ClearedUsed = result.UsedBefore - result.UsedAfter
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_reset_opportunity_accounts SET available_total=available_total-1,used_total=used_total+1,last_used_month=$2,updated_at=$3 WHERE user_id=$1`, user, month, s.now()); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_ledgers(user_id,related_subscription_id,change_type,delta,balance_after,used_month,source_type,source_ref,event_key,note,created_at,updated_at)
   VALUES($1,$2,'use',-1,$3,$4,'user_subscription',$5,$6,'使用额度重置机会清空当前订阅已用额度',$7,$7)`, user, result.SubscriptionID, available-1, month, strconv.FormatInt(result.SubscriptionID, 10), key, s.now()); err != nil {
			return err
		}
		result.ResetOpportunity, err = summary(ctx, tx, user, month)
		return err
	})
	return result, err
}
