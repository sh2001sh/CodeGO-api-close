package commerce

import (
	"context"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *Service) SetBeforeOrderHook(hook func(context.Context, pgx.Tx, Order) error) {
	s.cfg.BeforeOrder = hook
}

func (s *Service) freezeOrderRevenue(o *Order) error {
	p := s.topupPrice(o.Provider)
	if p.Currency != o.Currency || p.CreditsPerMinor <= 0 {
		o.RecognizedRevenueCredits = nil
		return nil
	}
	if o.AmountMinor < 0 || o.AmountMinor > math.MaxInt64/int64(p.CreditsPerMinor) {
		return credits.ErrOverflow
	}
	value := credits.Micro(o.AmountMinor) * p.CreditsPerMinor
	o.RecognizedRevenueCredits = &value
	return nil
}

func (s *Service) preparePaidOrderTx(ctx context.Context, tx pgx.Tx, o *Order) error {
	if err := s.freezeOrderRevenue(o); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE v3_commerce.orders SET recognized_revenue_credits=$2 WHERE id=$1`, o.ID, o.RecognizedRevenueCredits); err != nil {
		return err
	}
	if s.cfg.BeforeOrder != nil {
		if err := s.cfg.BeforeOrder(ctx, tx, *o); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT referral_terms FROM v3_commerce.orders WHERE id=$1`, o.ID).Scan(&o.ReferralTerms)
	}
	return nil
}

func (s *Service) SetOrderReleasedHook(hook func(context.Context, pgx.Tx, Order) error) {
	s.cfg.OrderReleased = hook
}
