package marketplace

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// standardDrawTx owns the entire standard branch, including zero-hour and
// pity advancement. The caller posts/fulfills its reward in this transaction.
func (s *Service) standardDrawTx(ctx context.Context, tx pgx.Tx, userID, purchaseID int64, p Pool, state *PityState) (Reward, string, error) {
	if !p.Standard.Enabled || p.Scope != "standard" || state == nil || userID <= 0 || purchaseID == 0 {
		return Reward{}, "none", ErrInvalidInput
	}
	if state.Opened == math.MaxInt64 || state.SmallProgress == math.MaxInt32 {
		return Reward{}, "none", credits.ErrOverflow
	}
	if err := validateStandard(p.Standard); err != nil {
		return Reward{}, "none", err
	}
	paid, first, err := standardPurchaseTx(ctx, tx, userID, purchaseID)
	if err != nil {
		return Reward{}, "none", err
	}
	first = first && p.Standard.FirstPurchaseMinimumMicro > 0
	due := p.Standard.PityAfter > 0 && state.SmallProgress >= p.Standard.PityAfter-1
	if r, hit, err := s.resolveStandardZeroHourTx(ctx, tx, userID, paid, first, due, state); err != nil {
		return Reward{}, "none", err
	} else if hit {
		return r, "none", nil
	}
	if r, hit, err := s.resolveStandardSubscriptionTx(ctx, tx, p, state); err != nil {
		return Reward{}, "none", err
	} else if hit {
		return r, "none", nil
	}
	r, guarantee, err := standardChooseBaseReward(p, s.cfg.Draw, due, first)
	if err != nil {
		return Reward{}, "none", err
	}
	r, err = normalizeStandardCreditsReward(r, first, due)
	if err != nil {
		return Reward{}, "none", err
	}
	advanceStandardPity(state, r, p.Standard)
	return r, guarantee, nil
}

// resolveStandardZeroHourTx runs the zero-hour hidden-reward check (only when
// neither a first-purchase nor pity draw is due) and otherwise unconditionally
// advances the paid zero-hour counter, mirroring the original inline order.
func (s *Service) resolveStandardZeroHourTx(ctx context.Context, tx pgx.Tx, userID int64, paid, first, due bool, state *PityState) (Reward, bool, error) {
	if !first && !due {
		r, hit, err := s.tryZeroHourTx(ctx, tx, userID, paid)
		if err != nil {
			return Reward{}, false, err
		}
		if hit {
			r.RewardTier = "zero_hour_hidden"
			state.Opened++
			state.SmallProgress, state.BigProgress = 0, 0
			return r, true, nil
		}
	}
	if err := s.advancePaidZeroHourTx(ctx, tx, userID, paid); err != nil {
		return Reward{}, false, err
	}
	return Reward{}, false, nil
}

// resolveStandardSubscriptionTx rolls the subscription-reward probability and,
// on a hit, loads the plan name and advances pity state for the subscription draw.
func (s *Service) resolveStandardSubscriptionTx(ctx context.Context, tx pgx.Tx, p Pool, state *PityState) (Reward, bool, error) {
	if p.Standard.SubscriptionProbabilityPPB <= 0 {
		return Reward{}, false, nil
	}
	n, err := s.cfg.Draw(1000000000)
	if err != nil {
		return Reward{}, false, err
	}
	if n < 0 || n >= 1000000000 {
		return Reward{}, false, ErrInvalidInput
	}
	if n >= p.Standard.SubscriptionProbabilityPPB {
		return Reward{}, false, nil
	}
	r := Reward{Kind: "subscription", PlanID: p.Standard.SubscriptionPlanID, Weight: 1, RewardTier: "subscription"}
	if err := tx.QueryRow(ctx, `SELECT name FROM v3_commerce.plans WHERE id=$1`, r.PlanID).Scan(&r.Title); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Reward{}, false, ErrNotFound
		}
		return Reward{}, false, err
	}
	state.Opened++
	state.SmallProgress, state.BigProgress = 0, 0
	return r, true, nil
}

// standardChooseBaseReward resolves the pity-minimum, randomly-drawn, or
// first-purchase-floored reward, before credit-field normalization.
func standardChooseBaseReward(p Pool, draw func(int64) (int64, error), due, first bool) (Reward, string, error) {
	if due {
		if p.Standard.PityMinimumMicro <= 0 {
			return Reward{}, "none", ErrUnavailable
		}
		r := Reward{Kind: "credits", Weight: 1, Amount: p.Standard.PityMinimumMicro, LegacyRewardType: "quota", WalletType: "default", RewardTier: "pity"}
		return r, "small", nil
	}
	r, err := standardChooseReward(p.Rewards, draw)
	if err != nil {
		return Reward{}, "none", err
	}
	if r.RewardTier == "" {
		r.RewardTier = r.Title
	}
	if first {
		r = standardFirstFloor(r, p.Standard)
		r.RewardTier = "first_purchase"
		return r, "first", nil
	}
	return r, "none", nil
}

// normalizeStandardCreditsReward fills in the wallet type, legacy reward type
// and display title for credits rewards, validating a positive amount.
func normalizeStandardCreditsReward(r Reward, first, due bool) (Reward, error) {
	if r.Kind != "credits" {
		return r, nil
	}
	if r.Amount <= 0 {
		return Reward{}, ErrInvalidInput
	}
	if r.LegacyRewardType == "" {
		r.LegacyRewardType = "quota"
	}
	if r.LegacyRewardType == "claude_quota" {
		r.WalletType = "claude"
	} else if r.WalletType != "claude" {
		r.WalletType = "default"
	}
	r.Title = fmt.Sprintf("%s 统一额度奖励", r.Amount.String())
	if first && !due {
		r.Title = fmt.Sprintf("首购专属奖励：%s 统一额度", r.Amount.String())
	}
	return r, nil
}

// advanceStandardPity advances the pity counters for a resolved reward,
// resetting the small-progress counter when the reward counts as high-value.
func advanceStandardPity(state *PityState, r Reward, policy StandardPolicy) {
	state.Opened++
	state.BigProgress = 0
	if standardHighValue(r, policy) {
		state.SmallProgress = 0
	} else {
		state.SmallProgress++
	}
}

// Legacy standard monetary tiers draw a continuous interval, then round to
// cents. Sampling every cent equally would double each endpoint's probability.
// Keep the original 53-bit draw precision and do the rounding with integers.
func standardChooseReward(rewards []Reward, draw func(int64) (int64, error)) (Reward, error) {
	tiers := append([]Reward(nil), rewards...)
	for i := range tiers {
		if tiers[i].Kind == "credits" && tiers[i].Amount == 0 {
			tiers[i].Amount = 1 // Select only the tier; sample its original interval below.
		}
	}
	r, err := chooseReward(tiers, draw)
	if err != nil || r.Kind != "credits" || r.Minimum == 0 {
		return r, err
	}
	if r.Minimum < 0 || r.Maximum < r.Minimum {
		return Reward{}, ErrInvalidInput
	}
	const precision = int64(1 << 53)
	var n int64
	if r.Maximum > r.Minimum {
		n, err = draw(precision)
		if err != nil {
			return Reward{}, err
		}
		if n < 0 || n >= precision {
			return Reward{}, ErrInvalidInput
		}
	}
	value := new(big.Int).Add(big.NewInt(int64(r.Minimum)), big.NewInt(5000))
	value.Mul(value, big.NewInt(precision))
	value.Add(value, new(big.Int).Mul(big.NewInt(n), big.NewInt(int64(r.Maximum-r.Minimum))))
	value.Quo(value, new(big.Int).Mul(big.NewInt(10000), big.NewInt(precision)))
	value.Mul(value, big.NewInt(10000))
	if !value.IsInt64() {
		return Reward{}, credits.ErrOverflow
	}
	r.Amount = credits.Micro(value.Int64())
	return r, nil
}

func standardFirstFloor(r Reward, p StandardPolicy) Reward {
	minimum := p.FirstPurchaseMinimumMicro
	if r.LegacyRewardType == "claude_quota" {
		minimum /= 4
	}
	if r.Kind != "credits" {
		return Reward{Kind: "credits", Weight: 1, Amount: minimum, LegacyRewardType: "quota", WalletType: "default"}
	}
	if r.Amount < minimum {
		r.Amount = minimum
		if r.LegacyRewardType != "claude_quota" {
			r.WalletType = "default"
		}
	}
	return r
}

func standardHighValue(r Reward, p StandardPolicy) bool {
	if r.Kind != "credits" || r.Amount <= 0 || p.LowRewardThresholdMicro <= 0 {
		return false
	}
	threshold := p.LowRewardThresholdMicro
	if r.LegacyRewardType == "claude_quota" {
		threshold = threshold/10 + (threshold%10+9)/10
	}
	return r.Amount >= threshold
}

func standardPurchaseTx(ctx context.Context, tx pgx.Tx, userID, purchaseID int64) (paid, first bool, err error) {
	var externalID int64
	var owner int64
	var grant bool
	err = tx.QueryRow(ctx, `SELECT user_id,is_grant,coalesce(external_order_id,0) FROM v3_marketplace.blind_box_purchases WHERE id=$1`, purchaseID).Scan(&owner, &grant, &externalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, ErrNotFound
	}
	if err != nil || owner != userID || grant {
		return false, false, err
	}
	if externalID == 0 {
		paid = true
		err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM v3_marketplace.blind_box_purchases p JOIN v3_marketplace.blind_box_pools b ON b.id=p.pool_id WHERE p.user_id=$1 AND NOT p.is_grant AND p.external_order_id IS NULL AND p.status='completed' AND b.scope='standard' AND p.id<$2) AND NOT EXISTS(SELECT 1 FROM v3_marketplace.blind_box_open_records r JOIN v3_marketplace.blind_box_items i ON i.id=r.item_id WHERE i.purchase_id=$2)`, userID, purchaseID).Scan(&first)
		return paid, first, err
	}
	var money, opened int64
	var source, status string
	err = tx.QueryRow(ctx, `SELECT amount_minor,opened_count,source,status FROM v3_marketplace.blind_box_orders WHERE id=$1 AND user_id=$2`, externalID, userID).Scan(&money, &opened, &source, &status)
	if err != nil {
		return false, false, err
	}
	paid = source == "purchase" && money > 0 && (status == "success" || status == "completed")
	if !paid || opened != 0 {
		return paid, false, nil
	}
	err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM v3_marketplace.blind_box_orders o LEFT JOIN v3_marketplace.blind_box_pools b ON b.id=o.pool_id WHERE o.user_id=$1 AND o.status IN('success','completed') AND o.amount_minor>0 AND o.id<$2 AND (b.scope='standard' OR o.pool_id IS NULL))`, userID, externalID).Scan(&first)
	return paid, first, err
}
