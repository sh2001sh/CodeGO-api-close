package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type walletConversionFacts struct {
	ID, User, Plan, Account                                    int64
	Order                                                      *int64
	Total, Used, Period, PeriodUsed, Balance, Spent, Renewable credits.Micro
	State, Policy                                              string
	StartsAt, ExpiresAt                                        time.Time
	NextReset                                                  *time.Time
	LastReset                                                  *time.Time
	ResetUsed                                                  bool
	ConvertedAt                                                *time.Time
	Snapshot                                                   Plan
	OrderAmount                                                int64
	OrderCredits                                               credits.Micro
	OrderCurrency                                              string
	OrderRevenue                                               *credits.Micro
	OriginalCredits                                            credits.Micro
	Complex                                                    bool
	LegacyPeriodic                                             bool
	ResetPeriod                                                string
	ResetSeconds                                               int64
	Sources                                                    []WalletConversionSource
}

func loadWalletConversionFacts(ctx context.Context, q rowQuerierCommerce, user, id int64, lock bool) (walletConversionFacts, error) {
	var f walletConversionFacts
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF s"
	}
	err := q.QueryRow(ctx, `SELECT s.id,s.user_id,s.plan_id,s.account_id,s.order_id,s.total_credits,s.used_credits,s.period_credits,s.period_used,a.balance,
 s.renewable_credits,s.state,s.policy_version,s.starts_at,s.expires_at,s.next_reset_at,s.last_reset_at,s.reset_opportunity_used,s.converted_at,s.plan_snapshot,
 COALESCE(o.amount_minor,0),COALESCE(o.credits,0),COALESCE(o.currency,''),o.recognized_revenue_credits,
 EXISTS(SELECT 1 FROM v3_commerce.subscription_fuel_fulfillments WHERE subscription_id=s.id AND NOT revoked)
 OR EXISTS(SELECT 1 FROM v3_commerce.subscription_conversions WHERE subscription_id=s.id)
 OR EXISTS(SELECT 1 FROM v3_commerce.user_refunds WHERE order_id=o.id AND status<>'failed'),s.legacy_periodic,s.reset_period,s.reset_custom_seconds
 FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id LEFT JOIN v3_commerce.orders o ON o.id=s.order_id
 WHERE s.id=$1 AND s.user_id=$2 AND s.deleted_at IS NULL`+suffix, id, user).Scan(&f.ID, &f.User, &f.Plan, &f.Account, &f.Order, &f.Total, &f.Used, &f.Period, &f.PeriodUsed, &f.Balance, &f.Renewable, &f.State, &f.Policy, &f.StartsAt, &f.ExpiresAt, &f.NextReset, &f.LastReset, &f.ResetUsed, &f.ConvertedAt, &f.Snapshot, &f.OrderAmount, &f.OrderCredits, &f.OrderCurrency, &f.OrderRevenue, &f.Complex, &f.LegacyPeriodic, &f.ResetPeriod, &f.ResetSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	if err != nil {
		return f, err
	}
	err = q.QueryRow(ctx, `SELECT COALESCE(GREATEST(-SUM(amount),0),0)::bigint FROM v3_billing.ledger_entries WHERE account_id=$1 AND kind IN('usage','refund')`, f.Account).Scan(&f.Spent)
	f.OriginalCredits = f.OrderCredits
	if f.OriginalCredits == 0 {
		f.OriginalCredits = f.Snapshot.Credits
	}
	if err != nil {
		return f, err
	}
	f.Sources, err = loadWalletConversionSources(ctx, q, f)
	return f, err
}

func digestValue(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func (f walletConversionFacts) basis() (string, error) {
	return digestValue(struct {
		Plan     int64
		Credits  credits.Micro
		Amount   int64
		Currency string
		Snapshot Plan
	}{f.Plan, f.OriginalCredits, f.OrderAmount, f.OrderCurrency, f.Snapshot})
}
func floorCreditRatio(value, num, den credits.Micro) (credits.Micro, error) {
	if value < 0 || num < 0 || den <= 0 {
		return 0, ErrInvalid
	}
	v := new(big.Int).Quo(new(big.Int).Mul(big.NewInt(int64(value)), big.NewInt(int64(num))), big.NewInt(int64(den)))
	if !v.IsInt64() {
		return 0, credits.ErrOverflow
	}
	return credits.Micro(v.Int64()), nil
}

func (s *Service) QuoteWalletConversion(ctx context.Context, user, id int64) (WalletConversionQuote, error) {
	return s.previewWalletConversion(ctx, user, id, true)
}
func (s *Service) previewWalletConversion(ctx context.Context, user, id int64, persist bool) (WalletConversionQuote, error) {
	out := WalletConversionQuote{SubscriptionID: id, State: "needs_review", TermsVersion: ConversionTerms}
	if user <= 0 || id <= 0 {
		return out, ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		f, err := loadWalletConversionFacts(ctx, tx, user, id, false)
		if err != nil {
			return err
		}
		if f.Policy != PolicyLegacy || f.State != "active" || f.ConvertedAt != nil || !f.ExpiresAt.After(s.cfg.Now()) || f.StartsAt.After(s.cfg.Now()) {
			return ErrStateConflict
		}
		if err = s.checkPackagePending(ctx, tx, id); err != nil {
			return err
		}
		out.BasisKey, err = f.basis()
		if err != nil {
			return err
		}
		out.SourceTotal = f.OriginalCredits
		out.SourceCredits = f.Balance
		out.SubscriptionExpiresAt = f.ExpiresAt
		if reviewed, err := s.previewReviewedWalletConversionTx(ctx, tx, f, &out, persist); reviewed || err != nil {
			return err
		}
		r, err := scanConversionRule(tx.QueryRow(ctx, `SELECT `+conversionRuleColumns+` FROM v3_commerce.subscription_conversion_rules WHERE plan_id=$1 AND source_credits=$2 AND basis_key=$3 AND enabled AND reviewed FOR SHARE`, f.Plan, f.OriginalCredits, out.BasisKey))
		if errors.Is(err, pgx.ErrNoRows) {
			out.ReviewReason = "当前老套餐版本尚未核定转换比例"
			return nil
		}
		if err != nil {
			return err
		}
		out.RuleID, out.RuleRevision = r.ID, r.Revision
		ppm, reason, err := quoteWalletConversionFacts(&out, f, r)
		if err != nil {
			return err
		}
		if reason != "" {
			out.ReviewReason = reason
			return nil
		}
		if !f.ExpiresAt.After(s.cfg.Now()) {
			return ErrStateConflict
		}
		out.State = "quoted"
		out.ExpiresAt = minTime(s.cfg.Now().Add(5*time.Minute), f.ExpiresAt)
		if !persist {
			return nil
		}
		out.QuoteID, err = tradeNumber()
		if err != nil {
			return err
		}
		hash, err := digestValue(f)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_wallet_quotes(quote_id,user_id,subscription_id,rule_id,rule_revision,source_account_id,original_order_id,original_total,source_credits,target_credits,paid_credits,revenue_multiplier_ppm,fact_hash,subscription_expires_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, out.QuoteID, user, id, r.ID, r.Revision, f.Account, f.Order, f.OriginalCredits, out.SourceCredits, out.TargetCredits, out.PaidCredits, ppm, hash, f.ExpiresAt, out.ExpiresAt)
		return err
	})
	return out, err
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func quoteWalletConversionFacts(out *WalletConversionQuote, f walletConversionFacts, r ConversionRule) (*int64, string, error) {
	used, err := f.Used.Add(f.Spent)
	if err != nil {
		return nil, "", err
	}
	if f.Complex || f.OriginalCredits <= 0 || f.Total != f.OriginalCredits || f.Balance <= 0 || f.Balance > f.Total || used < 0 || f.Balance != f.Total-used || f.Period > 0 || f.NextReset != nil {
		return nil, "历史补量、赠送、周期或来源需先人工核对", nil
	}
	paid := r.PaidWalletCredits
	if f.ResetUsed {
		if r.RefreshedPaidWalletCredits == nil {
			return nil, "已刷新权益的付费与赠送构成尚未核定", nil
		}
		paid = *r.RefreshedPaidWalletCredits
	}
	if f.Order == nil && paid > 0 {
		return nil, "无原实付订单，不能把赠送转换为本金", nil
	}
	out.TargetCredits, err = floorCreditRatio(f.Balance, r.WalletCredits, r.SourceCredits)
	if err != nil {
		return nil, "", err
	}
	out.PaidCredits, err = floorCreditRatio(f.Balance, paid, r.SourceCredits)
	if err != nil {
		return nil, "", err
	}
	if out.TargetCredits <= 0 {
		return nil, "剩余权益不足最小消费单位，不会扣除套餐", nil
	}
	out.RewardCredits = out.TargetCredits - out.PaidCredits
	revenue := f.OrderRevenue
	if revenue == nil {
		revenue = r.RecognizedRevenueCredits
	}
	if paid > 0 {
		if revenue == nil {
			return nil, "原实付收入尚未核定", nil
		}
		if paid > *revenue {
			return nil, "本金转换额超过已核定原实付收入", nil
		}
	}
	if out.PaidCredits > 0 {
		ratio, err := floorCreditRatio(*revenue, 1000000, paid)
		if err != nil {
			return nil, "", err
		}
		v := int64(ratio)
		return &v, "", nil
	}
	zero := int64(0)
	return &zero, "", nil
}
