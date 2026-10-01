package commerce

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// InitializeImportedMonthlyBenefits freezes pending imported checkout benefits
// before serving callbacks. Historical paid orders keep their migrated cards.
func (s *Service) InitializeImportedMonthlyBenefits(ctx context.Context) (int, error) {
	count := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		count, err = s.InitializeImportedMonthlyBenefitsTx(ctx, tx)
		return err
	})
	return count, err
}

// InitializeImportedMonthlyBenefitsTx shares the importer's atomic target transaction.
func (s *Service) InitializeImportedMonthlyBenefitsTx(ctx context.Context, tx pgx.Tx) (int, error) {
	count := 0
	err := func() error {
		rows, err := tx.Query(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders o
		 WHERE kind='subscription' AND state='created' AND purchase_type NOT IN ('fuel','subscription_fuel')
		 AND NOT EXISTS(SELECT 1 FROM v3_commerce.monthly_purchase_benefits b WHERE b.order_id=o.id)
		 ORDER BY id FOR UPDATE OF o`)
		if err != nil {
			return err
		}
		var pending []Order
		for rows.Next() {
			o, scanErr := scanOrder(rows)
			if scanErr != nil {
				rows.Close()
				return scanErr
			}
			pending = append(pending, o)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, o := range pending {
			// The source scales renewal time against the full plan price, not
			// its discounted amount due. Freeze that current imported rule.
			if err = tx.QueryRow(ctx, `SELECT price_minor FROM v3_commerce.plans WHERE id=$1 FOR SHARE`, o.PlanID).Scan(&o.AmountMinor); err != nil {
				return err
			}
			if err = s.FreezeMonthlyPurchaseBenefitsTx(ctx, tx, o); err != nil {
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
