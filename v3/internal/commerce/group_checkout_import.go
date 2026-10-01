package commerce

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// InitializeImportedGroupCheckouts binds still-payable imported orders before
// callbacks are admitted. A historical paid member is never enrolled again.
func (s *Service) InitializeImportedGroupCheckouts(ctx context.Context) (int, error) {
	count := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		count, err = s.InitializeImportedGroupCheckoutsTx(ctx, tx)
		return err
	})
	return count, err
}

// InitializeImportedGroupCheckoutsTx shares the importer's atomic target transaction.
func (s *Service) InitializeImportedGroupCheckoutsTx(ctx context.Context, tx pgx.Tx) (int, error) {
	count := 0
	err := func() error {
		rows, err := tx.Query(ctx, `SELECT o.id,o.user_id,o.purchase_type,
		 COALESCE(NULLIF(to_jsonb(o)->>'group_buy_id','')::bigint,0)
		 FROM v3_commerce.orders o LEFT JOIN v3_commerce.group_checkouts g ON g.order_id=o.id
		 WHERE g.order_id IS NULL AND o.kind='subscription' AND o.state IN('created','expired','canceled','failed')
		 AND o.purchase_type IN('group_buy','join_group') ORDER BY o.id`)
		if err != nil {
			return err
		}
		type imported struct {
			order Order
			group int64
		}
		var pending []imported
		for rows.Next() {
			var item imported
			if err = rows.Scan(&item.order.ID, &item.order.UserID, &item.order.PurchaseType, &item.group); err != nil {
				rows.Close()
				return err
			}
			pending = append(pending, item)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, item := range pending {
			if err = s.SaveGroupCheckoutTx(ctx, tx, item.order, item.group); err != nil {
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
