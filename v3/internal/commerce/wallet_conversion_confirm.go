package commerce

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

const walletConversionColumns = `request_id,quote_id,subscription_id,state,source_credits,target_credits,paid_credits,wallet_account_id,failure_reason,completed_at`

func scanWalletConversion(row scanner) (WalletConversion, error) {
	var c WalletConversion
	err := row.Scan(&c.RequestID, &c.QuoteID, &c.SubscriptionID, &c.State, &c.SourceCredits, &c.TargetCredits, &c.PaidCredits, &c.WalletAccountID, &c.FailureReason, &c.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return c, err
}
func (s *Service) WalletConversion(ctx context.Context, user int64, request string) (WalletConversion, error) {
	return scanWalletConversion(s.pool.QueryRow(ctx, `SELECT `+walletConversionColumns+` FROM v3_commerce.subscription_wallet_conversions WHERE request_id=$1 AND user_id=$2`, request, user))
}

func (s *Service) ConfirmWalletConversion(ctx context.Context, user int64, quote, request string, accepted bool) (WalletConversion, error) {
	if user <= 0 || !validOperation(quote) || !validOperation(request) || !accepted {
		return WalletConversion{}, ErrInvalid
	}
	key := "whole-wallet-conversion:" + request
	if err := s.authorizeWalletConversion(ctx, user, quote, request, key); err != nil {
		return WalletConversion{}, err
	}
	var terminal error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT subscription_id FROM v3_commerce.subscription_wallet_conversions WHERE request_id=$1 AND user_id=$2`, request, user).Scan(&id); err != nil {
			return err
		}
		if err := lockSubscriptionUserTx(ctx, tx, id); err != nil {
			return err
		}
		f, err := loadWalletConversionFacts(ctx, tx, user, id, true)
		if err != nil {
			return err
		}
		c, err := scanWalletConversion(tx.QueryRow(ctx, `SELECT `+walletConversionColumns+` FROM v3_commerce.subscription_wallet_conversions WHERE request_id=$1 AND user_id=$2 FOR UPDATE`, request, user))
		if err != nil {
			return err
		}
		if c.State == "completed" {
			return nil
		}
		if c.State == "failed" {
			terminal = ErrStateConflict
			return nil
		}
		var qAccount int64
		var hash string
		var ppm *int64
		if err = tx.QueryRow(ctx, `SELECT source_account_id,fact_hash,revenue_multiplier_ppm FROM v3_commerce.subscription_wallet_quotes WHERE quote_id=$1`, quote).Scan(&qAccount, &hash, &ppm); err != nil {
			return err
		}

		if s.cfg.FundingDrain != nil {
			ready, err := s.cfg.FundingDrain.FreezeAndDrained(ctx, tx, f.Account)
			if err != nil {
				return err
			}
			if !ready {
				return ErrFundingPending
			}
		}
		// Reload after drain: every last in-flight usage must be in the committed
		// ledger before comparing the immutable preview facts.
		f, err = loadWalletConversionFacts(ctx, tx, user, id, true)
		if err != nil {
			return err
		}
		actual, err := digestValue(f)
		if err != nil {
			return err
		}
		if f.State != "active" || f.ConvertedAt != nil || f.Policy != PolicyLegacy || !f.ExpiresAt.After(s.cfg.Now()) || f.Account != qAccount {
			return s.failWholeConversionTx(ctx, tx, f, c, key, "套餐已到期、状态或比例发生变化", &terminal)
		}
		if !f.ExpiresAt.After(s.cfg.Now()) || hash != actual || f.Balance != c.SourceCredits {
			return s.failWholeConversionTx(ctx, tx, f, c, key, "消费、刷新或到期使报价失效，请重新预览", &terminal)
		}
		return s.completeWholeConversionTx(ctx, tx, f, c, key, ppm)
	})
	out, loadErr := s.WalletConversion(ctx, user, request)
	return out, errors.Join(err, terminal, loadErr)
}

func (s *Service) authorizeWalletConversion(ctx context.Context, user int64, quote, request, key string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var found int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, user).Scan(&found); err != nil {
			return err
		}
		var priorQuote string
		var priorUser int64
		err := tx.QueryRow(ctx, `SELECT quote_id,user_id FROM v3_commerce.subscription_wallet_conversions WHERE request_id=$1`, request).Scan(&priorQuote, &priorUser)
		if err == nil {
			if priorQuote != quote || priorUser != user {
				return ErrStateConflict
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var id int64
		var source, target, paid credits.Micro
		var valid bool
		var reviewID, ruleID *int64
		var revision int64
		err = tx.QueryRow(ctx, `SELECT subscription_id,source_credits,target_credits,paid_credits,expires_at>$3,review_id,rule_id,rule_revision
 FROM v3_commerce.subscription_wallet_quotes WHERE quote_id=$1 AND user_id=$2 FOR SHARE`, quote, user, s.cfg.Now()).Scan(&id, &source, &target, &paid, &valid, &reviewID, &ruleID, &revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !valid {
			return ErrStateConflict
		}
		f, err := loadWalletConversionFacts(ctx, tx, user, id, true)
		if err != nil {
			return err
		}
		if f.State != "active" || !f.ExpiresAt.After(s.cfg.Now()) || f.ConvertedAt != nil {
			return ErrStateConflict
		}
		if reviewID != nil {
			err = tx.QueryRow(ctx, `SELECT enabled AND reviewed AND revision=$2 FROM v3_commerce.subscription_wallet_reviews WHERE id=$1 FOR SHARE`, *reviewID, revision).Scan(&valid)
		} else if ruleID != nil {
			err = tx.QueryRow(ctx, `SELECT enabled AND reviewed AND revision=$2 FROM v3_commerce.subscription_conversion_rules WHERE id=$1 FOR SHARE`, *ruleID, revision).Scan(&valid)
		} else {
			return ErrStateConflict
		}
		if err != nil {
			return err
		}
		if !valid {
			return ErrStateConflict
		}
		// Reuse this transaction's connection: competing user-row locks can
		// occupy every other pool slot, leaving a nested acquisition stranded.
		if err = s.allowSubscriptionConversion(ctx, tx, user, id, key); err != nil {
			return err
		}
		if err = s.checkPackagePending(ctx, tx, id); err != nil {
			return err
		}
		payload := map[string]any{"request_id": request, "wallet_quote_id": quote, "accepted_terms": true}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_operations(operation_id,subscription_id,actor_id,kind,payload) VALUES($1,$2,$3,'conversion',$4)`, key, id, user, payload); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_wallet_conversions(request_id,quote_id,operation_id,user_id,subscription_id,state,source_credits,target_credits,paid_credits,terms_version,accepted_at) VALUES($1,$2,$3,$4,$5,'pending',$6,$7,$8,$9,$10)`, request, quote, key, user, id, source, target, paid, ConversionTerms, s.cfg.Now())
		return err
	})
}

func (s *Service) failWholeConversionTx(ctx context.Context, tx pgx.Tx, f walletConversionFacts, c WalletConversion, key, reason string, terminal *error) error {
	*terminal = ErrStateConflict
	used, err := f.Used.Add(f.Spent)
	if err != nil {
		return err
	}
	periodUsed, err := f.PeriodUsed.Add(f.Spent)
	if err != nil {
		return err
	}
	if err = s.failConversion(ctx, tx, f.ID, f.User, f.Account, key, used, periodUsed, f.Balance); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_wallet_conversions SET state='failed',failure_reason=$2 WHERE request_id=$1`, c.RequestID, reason)
	return err
}

func (s *Service) completeWholeConversionTx(ctx context.Context, tx pgx.Tx, f walletConversionFacts, c WalletConversion, key string, ppm *int64) error {
	wallet, err := walletTx(ctx, tx, f.User)
	if err != nil {
		return err
	}
	if f.Balance > 0 {
		if _, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: f.Account, Amount: -f.Balance, Kind: "subscription_expire", OperationID: key + ":close", Reason: "legacy subscription converted voluntarily"}); err != nil {
			return err
		}
	}
	if err = s.postWalletConversionSegmentsTx(ctx, tx, f, c, wallet, key, ppm); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET state='canceled',converted_at=$2,ended_at=$2,benefits_until=expires_at,next_reset_at=NULL,reset_period='never',reset_custom_seconds=0,renewable_credits=0 WHERE id=$1`, f.ID, s.cfg.Now()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_buckets SET ended_at=$2 WHERE account_id=$1`, f.Account, s.cfg.Now()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_wallet_conversions SET state='completed',wallet_account_id=$2,completed_at=$3 WHERE request_id=$1`, c.RequestID, wallet, s.cfg.Now()); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_operations SET state='completed' WHERE operation_id=$1`, key)
	return err
}
