package commerce

import (
	"context"
	"math/big"

	"github.com/jackc/pgx/v5"
)

type importedCheckoutDiscount struct {
	order                                 int64
	paid                                  int64
	currency, state, original, multiplier string
	campaign                              bool
	start, end                            int64
	prop                                  *int64
	rate                                  *int64
}

// InitializeImportedCheckoutDiscounts runs offline after the source import.
// Existing reserved cards retain their exact order binding. Old topups did not
// retain their original price; original_known=false records that missing fact.
func (s *Service) InitializeImportedCheckoutDiscounts(ctx context.Context) (int, error) {
	count := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		count, err = s.InitializeImportedCheckoutDiscountsTx(ctx, tx)
		return err
	})
	return count, err
}

// InitializeImportedCheckoutDiscountsTx shares the importer's atomic target transaction.
func (s *Service) InitializeImportedCheckoutDiscountsTx(ctx context.Context, tx pgx.Tx) (int, error) {
	count := 0
	err := func() error {
		rows, err := tx.Query(ctx, `SELECT o.id,o.amount_minor,o.currency,o.state,
		 COALESCE(to_jsonb(o)->>'original_money','0'),COALESCE((to_jsonb(o)->>'discount_applied')::boolean,false),
		 COALESCE(to_jsonb(o)->>'discount_multiplier','0'),
		 COALESCE(extract(epoch FROM NULLIF(to_jsonb(o)->>'discount_starts_at','')::timestamptz)::bigint,0),
		 COALESCE(extract(epoch FROM NULLIF(to_jsonb(o)->>'discount_ends_at','')::timestamptz)::bigint,0),p.id,p.discount_rate_ppm
		 FROM v3_commerce.orders o LEFT JOIN v3_marketplace.blind_box_props p
		 ON p.user_id=o.user_id AND p.reserved_order_type=o.kind AND p.reserved_order_trade_no=o.trade_no AND p.status IN('reserved','used')
		 LEFT JOIN v3_commerce.checkout_discounts d ON d.order_id=o.id
		 WHERE d.order_id IS NULL AND o.kind IN('topup','subscription') AND o.purchase_type NOT IN('fuel','subscription_fuel')
		 AND (COALESCE((to_jsonb(o)->>'discount_applied')::boolean,false) OR p.id IS NOT NULL) ORDER BY o.id`)
		if err != nil {
			return err
		}
		var imported []importedCheckoutDiscount
		for rows.Next() {
			var v importedCheckoutDiscount
			if err = rows.Scan(&v.order, &v.paid, &v.currency, &v.state, &v.original, &v.campaign, &v.multiplier, &v.start, &v.end, &v.prop, &v.rate); err != nil {
				rows.Close()
				return err
			}
			imported = append(imported, v)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		seen := map[int64]bool{}
		for _, v := range imported {
			if seen[v.order] {
				return ErrInvalid
			}
			seen[v.order] = true
			d, err := v.snapshot()
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.checkout_discounts
			 (order_id,original_minor,original_known,paid_minor,campaign,multiplier,starts_at,ends_at,prop_id,state,created_at,updated_at)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`, d.OrderID, d.OriginalMinor, d.OriginalKnown, d.PaidMinor, d.Campaign, d.Multiplier, d.StartsAt, d.EndsAt, d.PropID, d.State, s.cfg.Now()); err != nil {
				return err
			}
			count++
		}
		return nil
	}()
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (v importedCheckoutDiscount) snapshot() (CheckoutDiscount, error) {
	d := CheckoutDiscount{OrderID: v.order, OriginalMinor: v.paid, PaidMinor: v.paid, Campaign: v.campaign, Multiplier: v.multiplier, StartsAt: v.start, EndsAt: v.end, PropID: v.prop, State: "reserved"}
	if v.paid <= 0 {
		return d, ErrInvalid
	}
	if v.campaign {
		if v.prop != nil {
			return d, ErrInvalid
		}
	} else {
		if v.prop == nil || v.rate == nil || *v.rate <= 0 || *v.rate > 1000000 {
			return d, ErrInvalid
		}
		rate := *v.rate
		if rate == 1000000 {
			rate = 990000
		}
		d.Multiplier = new(big.Rat).SetFrac(big.NewInt(1000000-rate), big.NewInt(1000000)).FloatString(6)
	}
	m, ok := new(big.Rat).SetString(d.Multiplier)
	if !ok || m.Sign() <= 0 || m.Cmp(big.NewRat(1, 1)) >= 0 {
		return d, ErrInvalid
	}
	original, ok := new(big.Rat).SetString(v.original)
	if !ok || original.Sign() < 0 {
		return d, ErrInvalid
	}
	if original.Sign() > 0 {
		original.Mul(original, new(big.Rat).SetInt64(paymentScale(v.currency)))
		if !original.IsInt() || !original.Num().IsInt64() || original.Num().Int64() < v.paid {
			return d, ErrInvalid
		}
		d.OriginalMinor, d.OriginalKnown = original.Num().Int64(), true
	}
	switch v.state {
	case "paid", "refunded":
		d.State = "consumed"
	case "expired", "failed", "canceled":
		if v.campaign {
			d.State = "released"
		}
	case "created":
	default:
		return d, ErrInvalid
	}
	return d, nil
}
