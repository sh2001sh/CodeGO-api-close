//go:build pgintegration

package marketplace

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestBoxOrdersLazyMaterializeLegacyRemainingInventory(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_pools SET scope='standard' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"success", "completed", "pending", "refunded"} {
		if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_orders(user_id,quantity,opened_count,trade_no,status,created_at) VALUES(1,5,2,$1,$2,$3)`, "legacy-"+status, status, f.s.cfg.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_orders(user_id,quantity,trade_no,status,expires_at) VALUES(1,5,'legacy-expired','completed',$1)`, f.s.cfg.Now()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			if err := lockUser(testContext, tx, 1); err != nil {
				return err
			}
			return f.s.EnsureExternalInventoryTx(testContext, tx, 1)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`); n != 6 {
		t.Fatalf("expected two legacy orders × three remaining, got %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_purchases WHERE external_order_id IS NOT NULL AND quantity=5`); n != 2 {
		t.Fatalf("duplicate/lost materialization markers: %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_orders WHERE trade_no IN('legacy-success','legacy-completed') AND opened_count=2 AND pool_id=$1`, p.ID); n != 2 {
		t.Fatal("materialization changed historic opened_count or chose wrong scope")
	}
}

func TestBoxOrdersQuoteConflictLimitsAndCreationRollback(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	p.DailyLimit = 2
	if _, err := f.s.SavePool(testContext, p); err != nil {
		t.Fatal(err)
	}
	o := boxOrderFixture(t, f, p.ID, "quote", 2)
	in := o.BoxOrderInput
	in.Quantity = 1
	err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, err := f.s.CreateBoxOrderTx(testContext, tx, in)
		return err
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("changed quote accepted: %v", err)
	}
	in.TradeNo = "over-limit"
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, err := f.s.CreateBoxOrderTx(testContext, tx, in)
		return err
	})
	if !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("pending order did not count against daily limit: %v", err)
	}
	if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		return f.s.CancelBoxOrderTx(testContext, tx, 1, o.TradeNo)
	}); err != nil {
		t.Fatal(err)
	}
	in.TradeNo = "rollback-create"
	injected := errors.New("commerce creation failed")
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		if _, err := f.s.CreateBoxOrderTx(testContext, tx, in); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_orders WHERE trade_no=$1`, in.TradeNo) != 0 {
		t.Fatalf("creation rollback failed: %v", err)
	}
	expiry := f.s.cfg.Now().Add(-time.Second)
	in.ExpiresAt = &expiry
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, err := f.s.CreateBoxOrderTx(testContext, tx, in)
		return err
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expired grant created: %v", err)
	}
}
