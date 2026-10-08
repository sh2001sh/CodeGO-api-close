package commerce

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *Service) postWalletConversionSegmentsTx(ctx context.Context, tx pgx.Tx, f walletConversionFacts, c WalletConversion, wallet int64, key string, ppm *int64) error {
	var segments []WalletConversionSegment
	if err := tx.QueryRow(ctx, `SELECT segments FROM v3_commerce.subscription_wallet_quotes WHERE quote_id=$1`, c.QuoteID).Scan(&segments); err != nil {
		return err
	}
	if len(segments) == 0 {
		order := int64(0)
		if f.Order != nil {
			order = *f.Order
		}
		segment := WalletConversionSegment{OriginalOrderID: order, TargetCredits: c.TargetCredits, PaidCredits: c.PaidCredits}
		if ppm != nil {
			segment.RevenueMultiplierPPM = *ppm
		} else if c.PaidCredits > 0 {
			return fmt.Errorf("commerce: missing frozen conversion revenue")
		}
		segments = []WalletConversionSegment{segment}
	}
	var total, paid credits.Micro
	for i, p := range segments {
		if p.TargetCredits < 0 || p.PaidCredits < 0 || p.PaidCredits > p.TargetCredits {
			return ErrStateConflict
		}
		var err error
		total, err = total.Add(p.TargetCredits)
		if err == nil {
			paid, err = paid.Add(p.PaidCredits)
		}
		if err != nil {
			return err
		}
		for _, part := range []struct {
			amount credits.Micro
			paid   bool
		}{{p.PaidCredits, true}, {p.TargetCredits - p.PaidCredits, false}} {
			if part.amount == 0 {
				continue
			}
			meta := map[string]any{"source": "subscription_conversion", "subscription_id": f.ID, "original_order_id": p.OriginalOrderID, "paid_principal_credits": int64(0), "reward_credits": int64(0), "conversion_segment": p.Name}
			if part.paid {
				meta["paid_principal_credits"] = int64(part.amount)
				meta["revenue_multiplier_ppm"] = p.RevenueMultiplierPPM
			} else {
				meta["reward_credits"] = int64(part.amount)
				meta["non_transferable"], meta["non_refundable"] = true, true
			}
			operation := fmt.Sprintf("%s:wallet:%t", key, part.paid)
			if len(segments) > 1 || p.Name != "" {
				operation = fmt.Sprintf("%s:segment:%d:%t", key, i, part.paid)
			}
			if _, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: wallet, Amount: part.amount, Kind: "transfer", OperationID: operation, Reason: "legacy subscription conversion", Metadata: meta}); err != nil {
				return err
			}
		}
	}
	if total != c.TargetCredits || paid != c.PaidCredits {
		return ErrStateConflict
	}
	return nil
}
