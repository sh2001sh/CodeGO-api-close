//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestRedesignOrderReservationReleaseAndTopupPaidHooksAreAtomic(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	s.SetBeforeOrderHook(func(ctx context.Context, tx pgx.Tx, o commerce.Order) error {
		if o.RecognizedRevenueCredits == nil {
			return commerce.ErrInvalid
		}
		_, err := tx.Exec(ctx, `UPDATE v3_identity.users SET settings=settings||jsonb_build_object('reserved_order',$2::bigint) WHERE id=$1`, o.UserID, o.ID)
		return err
	})
	s.SetOrderReleasedHook(func(ctx context.Context, tx pgx.Tx, o commerce.Order) error {
		_, err := tx.Exec(ctx, `UPDATE v3_identity.users SET settings=settings||jsonb_build_object('released_order',$2::bigint) WHERE id=$1`, o.UserID, o.ID)
		return err
	})
	s.SetPaidPurchaseHook(func(ctx context.Context, tx pgx.Tx, o commerce.Order) error {
		if o.PlanID != nil {
			return commerce.ErrInvalid
		}
		_, err := tx.Exec(ctx, `UPDATE v3_identity.users SET settings=settings||jsonb_build_object('paid_topup',$2::bigint) WHERE id=$1`, o.UserID, o.ID)
		return err
	})
	canceled := create(t, s, 0)
	if err := s.Cancel(ctx, 1, canceled.TradeNo); err != nil {
		t.Fatal(err)
	}
	var released int64
	if err := pool.QueryRow(ctx, `SELECT (settings->>'released_order')::bigint FROM v3_identity.users WHERE id=1`).Scan(&released); err != nil || released != canceled.ID {
		t.Fatalf("cancel didn't release %d %v", released, err)
	}
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	var paid int64
	if err := pool.QueryRow(ctx, `SELECT (settings->>'paid_topup')::bigint FROM v3_identity.users WHERE id=1`).Scan(&paid); err != nil || paid != o.ID {
		t.Fatalf("topup paid hook=%d %v", paid, err)
	}
	expired := create(t, s, 0)
	*now = expired.ExpiresAt
	if n, err := s.ExpireOrders(ctx); err != nil || n != 1 {
		t.Fatalf("expiry=%d %v", n, err)
	}
	if err := pool.QueryRow(ctx, `SELECT (settings->>'released_order')::bigint FROM v3_identity.users WHERE id=1`).Scan(&released); err != nil || released != expired.ID {
		t.Fatalf("expiry didn't release %d %v", released, err)
	}
	s.SetBeforeOrderHook(func(context.Context, pgx.Tx, commerce.Order) error { return commerce.ErrStateConflict })
	if _, err := s.Create(ctx, commerce.CreateOrder{UserID: 1, AmountMinor: 1000, Provider: "test", SuccessURL: "https://site.test/ok", CancelURL: "https://site.test/cancel"}); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("failed reservation proceeds %v", err)
	}
	var orders int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.orders`).Scan(&orders); err != nil || orders != 3 {
		t.Fatalf("failed hook persisted order %d %v", orders, err)
	}
}
