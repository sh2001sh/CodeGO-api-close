//go:build pgintegration

package marketplace

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func boxOrderFixture(t *testing.T, f *fixture, pool int64, trade string, count int) BoxOrder {
	t.Helper()
	var order BoxOrder
	err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		var err error
		order, err = f.s.CreateBoxOrderTx(testContext, tx, BoxOrderInput{UserID: 1, PoolID: pool,
			Quantity: count, AmountMinor: 250 * int64(count), TradeNo: trade, Currency: "usd", PaymentProvider: "stripe"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return order
}

func TestBoxOrdersVerifiedPaymentReplayAndConcurrentCompletion(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	o := boxOrderFixture(t, f, p.ID, "cash-1", 3)
	if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		replay, err := f.s.CreateBoxOrderTx(testContext, tx, o.BoxOrderInput)
		if err == nil && replay.ID != o.ID {
			t.Errorf("replay allocated %d, expected %d", replay.ID, o.ID)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		user, amount int64
		currency     string
	}{{1, 749, "usd"}, {1, 750, "eur"}, {2, 750, "usd"}} {
		err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			_, err := f.s.CompleteBoxOrderTx(testContext, tx, bad.user, o.TradeNo, bad.amount, bad.currency)
			return err
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("mismatch was accepted: %v", err)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items`); n != 0 {
		t.Fatalf("unverified inventory: %d", n)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
				_, err := f.s.CompleteBoxOrderTx(testContext, tx, 1, o.TradeNo, 750, "USD")
				return err
			})
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items`); n != 3 {
		t.Fatalf("duplicate inventory: %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_purchases WHERE external_order_id=$1`, o.ID); n != 1 {
		t.Fatalf("purchase marker: %d", n)
	}
	if balance := f.balance(t, 1); balance != 10000 {
		t.Fatalf("cash payment also charged wallet: %d", balance)
	}
}

func TestBoxOrdersRollbackAndRefundGuards(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	o := boxOrderFixture(t, f, p.ID, "rollback-cash", 2)
	injected := errors.New("fulfillment failure")
	err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		if _, err := f.s.CompleteBoxOrderTx(testContext, tx, 1, o.TradeNo, o.AmountMinor, o.Currency); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items`) != 0 ||
		f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_orders WHERE id=$1 AND status='pending'`, o.ID) != 1 {
		t.Fatalf("failed fulfillment retained state: %v", err)
	}
	var purchase Purchase
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		var err error
		purchase, err = f.s.CompleteBoxOrderTx(testContext, tx, 1, o.TradeNo, o.AmountMinor, o.Currency)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			return f.s.CancelBoxOrderTx(testContext, tx, 1, o.TradeNo)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE purchase_id=$1 AND status='revoked'`, purchase.ID) != 2 {
		t.Fatal("refund did not revoke all inventory")
	}
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, err := f.s.CompleteBoxOrderTx(testContext, tx, 1, o.TradeNo, o.AmountMinor, o.Currency)
		return err
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("late fulfillment recreated refunded inventory: %v", err)
	}
}

func TestBoxOrdersConsumptionAndGiftBlockRefund(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	for _, trade := range []string{"opened-cash", "gifted-cash"} {
		o := boxOrderFixture(t, f, p.ID, trade, 1)
		err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			purchase, err := f.s.CompleteBoxOrderTx(testContext, tx, 1, o.TradeNo, o.AmountMinor, o.Currency)
			if err != nil {
				return err
			}
			if trade == "gifted-cash" {
				_, err = tx.Exec(testContext, `UPDATE v3_marketplace.blind_box_items SET owner_user_id=2 WHERE purchase_id=$1`, purchase.ID)
				return err
			}
			var itemID, recordID int64
			if err := tx.QueryRow(testContext, `SELECT id FROM v3_marketplace.blind_box_items WHERE purchase_id=$1 FOR UPDATE`, purchase.ID).Scan(&itemID); err != nil {
				return err
			}
			if err := tx.QueryRow(testContext, `INSERT INTO v3_marketplace.blind_box_open_records(item_id,user_id,request_id,reward) VALUES($1,1,'open-cash','{}') RETURNING id`, itemID).Scan(&recordID); err != nil {
				return err
			}
			if err := f.s.RecordExternalOpenTx(testContext, tx, purchase.ID, recordID); err != nil {
				return err
			}
			_, err = tx.Exec(testContext, `UPDATE v3_marketplace.blind_box_items SET status='opened',opened_at=$2 WHERE id=$1`, itemID, f.s.cfg.Now())
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			return f.s.CancelBoxOrderTx(testContext, tx, 1, trade)
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("%s refunded: %v", trade, err)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_orders WHERE trade_no='opened-cash' AND opened_count=1`); n != 1 {
		t.Fatal("opening did not preserve external order counter")
	}
}

func TestBoxOrdersPreserveSubscriptionBenefitAndDeadline(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	expiry := f.s.cfg.Now().Add(time.Hour)
	in := BoxOrderInput{UserID: 1, PoolID: p.ID, Quantity: 2, TradeNo: "benefit-1", Currency: "usd",
		Source: "subscription_benefit", SubscriptionID: 42, BenefitCycle: "2026-09", ExpiresAt: &expiry}
	err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		o, err := f.s.CreateBoxOrderTx(testContext, tx, in)
		if err != nil {
			return err
		}
		_, err = f.s.CompleteBoxOrderTx(testContext, tx, 1, o.TradeNo, 0, "usd")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_orders WHERE source='subscription_benefit' AND subscription_id=42 AND benefit_cycle='2026-09' AND expires_at=$1`, expiry); n != 1 {
		t.Fatal("benefit metadata lost")
	}
	f.now.Add(3600)
	// A provider callback retry after the inventory expiry keeps its prior
	// fulfillment result, but cannot reactivate the expired inventory.
	if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, err := f.s.CompleteBoxOrderTx(testContext, tx, 1, in.TradeNo, 0, "usd")
		return err
	}); err != nil {
		t.Fatalf("expired fulfillment replay failed: %v", err)
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE expires_at>$1`, f.s.cfg.Now()) != 0 {
		t.Fatal("expired fulfillment replay reactivated inventory")
	}
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		var purchaseID, recordID int64
		if err := tx.QueryRow(testContext, `SELECT id FROM v3_marketplace.blind_box_purchases`).Scan(&purchaseID); err != nil {
			return err
		}
		if err := tx.QueryRow(testContext, `INSERT INTO v3_marketplace.blind_box_open_records(user_id,request_id,reward) VALUES(1,'expired-test','{}') RETURNING id`).Scan(&recordID); err != nil {
			return err
		}
		return f.s.RecordExternalOpenTx(testContext, tx, purchaseID, recordID)
	})
	if !errors.Is(err, ErrConflict) || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records`) != 0 {
		t.Fatalf("expired grant consumed or left partial record: %v", err)
	}
}
