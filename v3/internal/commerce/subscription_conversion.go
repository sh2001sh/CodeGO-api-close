package commerce

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type SubscriptionConversion struct {
	RequestID         string        `json:"request_id"`
	SubscriptionID    int64         `json:"subscription_id"`
	ConversionPercent int           `json:"conversion_percent"`
	SourceCredits     credits.Micro `json:"source_credits"`
	TargetCredits     credits.Micro `json:"target_credits"`
}

func conversionQuote(p Plan, total, used credits.Micro, percent int) (credits.Micro, credits.Micro, int, error) {
	if total <= 0 || used >= total || p.PriceMinor <= 0 || (p.PlanType != "monthly" && p.DurationUnit != "month") {
		return 0, 0, 0, ErrInvalid
	}
	remaining := total - used
	maxPercent := new(big.Int).Quo(new(big.Int).Mul(big.NewInt(int64(remaining)), big.NewInt(100)), big.NewInt(int64(total))).Int64()
	maxPercent = max(maxPercent, 1)
	if percent == 0 {
		percent = int(maxPercent)
	}
	if percent < 1 || int64(percent) > maxPercent {
		return 0, 0, int(maxPercent), ErrInvalid
	}
	numerator, denominator := big.NewInt(int64(percent)), big.NewInt(100)
	source := new(big.Int).Quo(new(big.Int).Mul(big.NewInt(int64(total)), numerator), denominator).Int64()
	if int64(percent) == maxPercent {
		source = int64(remaining)
		numerator, denominator = big.NewInt(source), big.NewInt(int64(total))
	}
	value := new(big.Int).Mul(big.NewInt(p.PriceMinor), numerator)
	value.Mul(value, big.NewInt(credits.PerCredit))
	divisor := new(big.Int).Mul(denominator, big.NewInt(paymentScale(p.Currency)))
	target := new(big.Int).Quo(value, divisor)
	if !target.IsInt64() {
		return 0, 0, int(maxPercent), credits.ErrOverflow
	}
	amount := target.Int64()
	if amount == 0 && int64(percent) == maxPercent {
		amount = 1
	}
	if source <= 0 || source > int64(remaining) || amount <= 0 {
		return 0, 0, int(maxPercent), ErrInvalid
	}
	return credits.Micro(source), credits.Micro(amount), int(maxPercent), nil
}

func (s *Service) ConvertSubscription(ctx context.Context, user, id int64, percent int, request string) (SubscriptionConversion, error) {
	result := SubscriptionConversion{RequestID: request, SubscriptionID: id, ConversionPercent: percent}
	if user <= 0 || id <= 0 || percent < 1 || percent > 100 || !validOperation(request) {
		return result, ErrInvalid
	}
	key := "subscription-conversion:" + request
	if err := s.allowSubscriptionConversion(ctx, user, id, key); err != nil {
		return result, err
	}
	// Validate ownership before recording a recoverable, authorized intent.
	var owned bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscriptions WHERE id=$1 AND user_id=$2)`, id, user).Scan(&owned); err != nil {
		return result, err
	}
	if !owned {
		return result, ErrNotFound
	}
	if err := s.queueSubscriptionOperation(ctx, key, id, user, "conversion", map[string]any{"request_id": request, "percent": percent}); err != nil {
		return result, err
	}
	var terminal error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return s.convertSubscriptionTx(ctx, tx, user, id, percent, key, &result, &terminal)
	})
	return result, errors.Join(err, terminal)
}

// convertSubscriptionTx holds the subscription-and-operation lock order from
// the original monolithic transaction: subscription row, then operation row,
// then plan, then eligibility, then funding drain, then usage.
func (s *Service) convertSubscriptionTx(ctx context.Context, tx pgx.Tx, user, id int64, percent int, key string, result *SubscriptionConversion, terminal *error) error {
	if err := lockSubscriptionUserTx(ctx, tx, id); err != nil {
		return err
	}
	var account, plan int64
	var totalC, usedC, periodUsedC credits.Micro
	var state string
	if err := tx.QueryRow(ctx, `SELECT account_id,plan_id,total_credits,used_credits,period_used,state FROM v3_commerce.subscriptions WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, user).Scan(&account, &plan, &totalC, &usedC, &periodUsedC, &state); err != nil {
		return err
	}
	done, err := resolveConversionOperation(ctx, tx, key, result)
	if err != nil || done {
		return err
	}
	p, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1 FOR SHARE`, plan))
	if err != nil {
		return err
	}
	eligible, reset, err := checkConversionEligibility(ctx, tx, id, s.cfg.Now())
	if err != nil {
		return err
	}
	if !eligible || reset {
		*terminal = ErrStateConflict
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_operations SET state='failed' WHERE operation_id=$1`, key)
		return err
	}
	if s.cfg.FundingDrain != nil {
		drained, err := s.cfg.FundingDrain.FreezeAndDrained(ctx, tx, account)
		if err != nil {
			return err
		}
		if !drained {
			return ErrFundingPending
		}
	}
	spent, balance, err := loadConversionUsage(ctx, tx, account)
	if err != nil {
		return err
	}
	if usedC, err = usedC.Add(spent); err != nil {
		return err
	}
	if periodUsedC, err = periodUsedC.Add(spent); err != nil {
		return err
	}
	source, target, _, quoteErr := conversionQuote(p, totalC, usedC, percent)
	if quoteErr != nil || source > balance {
		*terminal = ErrInvalid
		return s.failConversion(ctx, tx, id, user, account, key, usedC, periodUsedC, balance)
	}
	return s.completeConversion(ctx, tx, conversionCompleteParams{id: id, user: user, account: account, key: key, percent: percent,
		state: state, total: totalC, used: usedC, periodUsed: periodUsedC, balance: balance, source: source, target: target, result: result})
}

// resolveConversionOperation locks the operation row and short-circuits if a
// prior attempt already settled it, returning the stored result on success.
func resolveConversionOperation(ctx context.Context, tx pgx.Tx, key string, result *SubscriptionConversion) (bool, error) {
	var operationState string
	if err := tx.QueryRow(ctx, `SELECT state FROM v3_commerce.subscription_operations WHERE operation_id=$1 FOR UPDATE`, key).Scan(&operationState); err != nil {
		return false, err
	}
	if operationState == "completed" {
		err := tx.QueryRow(ctx, `SELECT source_credits,target_credits FROM v3_commerce.subscription_conversions WHERE operation_id=$1`, key).Scan(&result.SourceCredits, &result.TargetCredits)
		return true, err
	}
	if operationState == "failed" {
		return false, ErrStateConflict
	}
	return false, nil
}

func checkConversionEligibility(ctx context.Context, tx pgx.Tx, id int64, now time.Time) (eligible, reset bool, err error) {
	if err = tx.QueryRow(ctx, `SELECT expires_at>$2 AND state='active' AND deleted_at IS NULL AND policy_version='legacy' AND converted_at IS NULL FROM v3_commerce.subscriptions WHERE id=$1`, id, now).Scan(&eligible); err != nil {
		return
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.package_checkouts WHERE target_subscription_id=$1 AND state IN ('preparing','checkout'))`, id).Scan(&pending); err != nil {
		return
	}
	if pending {
		err = ErrFundingPending
		return
	}
	err = tx.QueryRow(ctx, `SELECT reset_opportunity_used FROM v3_commerce.subscriptions WHERE id=$1`, id).Scan(&reset)
	return
}

func loadConversionUsage(ctx context.Context, tx pgx.Tx, account int64) (spent, balance credits.Micro, err error) {
	if err = tx.QueryRow(ctx, `SELECT COALESCE(GREATEST(-SUM(amount),0),0)::bigint FROM v3_billing.ledger_entries WHERE account_id=$1 AND kind IN ('usage','refund')`, account).Scan(&spent); err != nil {
		return
	}
	err = tx.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, account).Scan(&balance)
	return
}

// failConversion restores the subscription's bucket to its pre-attempt
// balance and marks the operation failed when the quote cannot be honored.
func (s *Service) failConversion(ctx context.Context, tx pgx.Tx, id, user, account int64, key string, used, periodUsed, balance credits.Micro) error {
	fresh, err := s.rotateSubscriptionBucket(ctx, tx, id, user, account, max(balance, 0), key+":restore", s.cfg.Now())
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET account_id=$2,used_credits=$3,period_used=$4 WHERE id=$1`, id, fresh, int64(used), int64(periodUsed)); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_operations SET state='failed' WHERE operation_id=$1`, key)
	return err
}

type conversionCompleteParams struct {
	id, user, account       int64
	key                     string
	percent                 int
	state                   string
	total, used, periodUsed credits.Micro
	balance, source, target credits.Micro
	result                  *SubscriptionConversion
}

// completeConversion debits the subscription bucket, credits the wallet, and
// records the conversion once the quote has been validated against balance.
func (s *Service) completeConversion(ctx context.Context, tx pgx.Tx, p conversionCompleteParams) error {
	fresh, err := s.rotateSubscriptionBucket(ctx, tx, p.id, p.user, p.account, p.balance-p.source, p.key, s.cfg.Now())
	if err != nil {
		return err
	}
	used, err := p.used.Add(p.source)
	if err != nil {
		return err
	}
	periodUsed, err := p.periodUsed.Add(p.source)
	if err != nil {
		return err
	}
	state := p.state
	if used >= p.total {
		state = "canceled"
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET account_id=$2,used_credits=$3,period_used=$4,state=$5,ended_at=CASE WHEN $5='canceled' THEN $6::timestamptz ELSE ended_at END WHERE id=$1`, p.id, fresh, int64(used), int64(periodUsed), state, s.cfg.Now()); err != nil {
		return err
	}
	if state == "canceled" {
		if err = RestoreSubscriptionGroupTx(ctx, tx, p.id, s.cfg.Now()); err != nil {
			return err
		}
	}
	wallet, err := walletTx(ctx, tx, p.user)
	if err != nil {
		return err
	}
	if _, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: wallet, Amount: p.target, Kind: "transfer", OperationID: p.key + ":wallet", Reason: "monthly subscription converted to unified credit", Metadata: map[string]any{"subscription_id": p.id, "source_credits": int64(p.source), "conversion_percent": p.percent}}); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_conversions(operation_id,user_id,subscription_id,conversion_percent,source_credits,target_credits,cycle_order_id)
	 SELECT $1,$2,id,$4,$5,$6,COALESCE(order_id,0) FROM v3_commerce.subscriptions WHERE id=$3`, p.key, p.user, p.id, p.percent, int64(p.source), int64(p.target)); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_operations SET state='completed' WHERE operation_id=$1`, p.key)
	p.result.SourceCredits, p.result.TargetCredits = p.source, p.target
	return err
}

func (s *Service) ListSubscriptionConversions(ctx context.Context, user int64, limit int) ([]SubscriptionConversion, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `SELECT op.payload->>'request_id',c.subscription_id,c.conversion_percent,c.source_credits,c.target_credits FROM v3_commerce.subscription_conversions c JOIN v3_commerce.subscription_operations op USING(operation_id) WHERE c.user_id=$1 ORDER BY c.created_at DESC LIMIT $2`, user, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SubscriptionConversion, 0)
	for rows.Next() {
		var row SubscriptionConversion
		if err = rows.Scan(&row.RequestID, &row.SubscriptionID, &row.ConversionPercent, &row.SourceCredits, &row.TargetCredits); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
