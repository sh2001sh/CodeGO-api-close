//go:build pgintegration

package marketplace

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestDiscountReservationRollbackReplayAndRelease(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "subscription_discount", Title: "discount", DiscountRatePPM: 100000})
	if _, err := f.s.GrantBoxes(testContext, 1, 1, "seed", p.ID, 2); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_identity.users SET role='admin' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GrantBoxes(testContext, 1, 1, "seed", p.ID, 2); err != nil {
		t.Fatal(err)
	}
	records, err := f.s.OpenBoxes(testContext, 1, "open", 2)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDiscounts(f.s.cfg.Now)
	var reserved Discount
	rollback := errors.New("order insert failed")
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		reserved, err = d.ReserveDiscountTx(testContext, tx, 1, "subscription", "trade-a")
		if err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_props WHERE status='reserved'`) != 0 {
		t.Fatal("reservation survived rollback")
	}
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		var err error
		reserved, err = d.ReserveDiscountTx(testContext, tx, 1, "subscription", "trade-a")
		if err != nil {
			return err
		}
		replay, err := d.ReserveDiscountTx(testContext, tx, 1, "subscription", "trade-a")
		if err != nil {
			return err
		}
		if replay != reserved || reserved.PropID != records[0].PropID {
			t.Fatal("replay allocated another prop")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		return d.ConsumeDiscountTx(testContext, tx, 2, reserved.PropID, "subscription", "trade-a")
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		if err := d.ReleaseDiscountTx(testContext, tx, 1, reserved.PropID, "subscription", "trade-a"); err != nil {
			return err
		}
		return d.ReleaseDiscountTx(testContext, tx, 1, reserved.PropID, "subscription", "trade-a")
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		a, err := d.ReserveDiscountTx(testContext, tx, 1, "subscription", "trade-b")
		if err != nil {
			return err
		}
		if a.PropID != reserved.PropID {
			t.Fatal("released prop lost")
		}
		if err := d.ConsumeDiscountTx(testContext, tx, 1, a.PropID, "subscription", "trade-b"); err != nil {
			return err
		}
		if err := d.ConsumeDiscountTx(testContext, tx, 1, a.PropID, "subscription", "trade-b"); err != nil {
			return err
		}
		replay, err := d.ReserveDiscountTx(testContext, tx, 1, "subscription", "trade-b")
		if err != nil {
			return err
		}
		if replay != a {
			t.Fatal("used order replay allocated again")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_props WHERE status='used'`) != 1 {
		t.Fatal("discount consumed twice")
	}
}

func TestRetainedCappedDiscountAuditReplayAndExhaustion(t *testing.T) {
	f := newFixture(t)
	// A previously retained card can still be audited, but new native pools
	// reject caps because current billing does not reserve their budgets.
	var id int64
	if err := f.pool.QueryRow(testContext, `INSERT INTO v3_marketplace.blind_box_props(user_id,kind,title,status,multiplier_ppm,duration_seconds,remaining_seconds,max_discount_micro,expires_at)
	 VALUES(1,'multiplier','retained capped','active',100000,3600,3600,900,$1) RETURNING id`, f.s.cfg.Now().Add(time.Hour)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	record := func(request string, before, after int64) error {
		return pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			return f.s.RecordDiscountUsageTx(testContext, tx, 1, id, 9, request, credits.Micro(before), credits.Micro(after))
		})
	}
	if err := record("request", 500, 50); err != nil {
		t.Fatal(err)
	}
	if err := record("request", 500, 50); err != nil {
		t.Fatal(err)
	}
	if err := record("request", 501, 50); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := record("overflow", 1000, 0); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := record("second", 500, 50); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_prop_discount_usages`) != 2 {
		t.Fatal("audit count")
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_props WHERE id=$1 AND used_discount_micro=900 AND status='used'`, id) != 1 {
		t.Fatal("cap not exhausted")
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.account_profiles WHERE user_id=1 AND multiplier_ppm=1000000 AND cards='[]'::jsonb`) != 1 {
		t.Fatal("exhausted card still active")
	}
}
