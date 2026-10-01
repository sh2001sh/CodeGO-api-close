//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestPartialRefundsAreCumulativeConcurrentAndFinishExactly(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := s.ConfirmRefundTotal(ctx, "test", o.TradeNo, "partial-1", o.Currency, 400); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	// An older callback delivered late cannot put credits back or debit twice.
	if err := s.ConfirmRefundTotal(ctx, "test", o.TradeNo, "partial-old", o.Currency, 200); err != nil {
		t.Fatal(err)
	}
	assert := func(balance int64, state string, reversals int) {
		t.Helper()
		var actual int64
		var count int
		var actualState string
		if err := pool.QueryRow(ctx, `SELECT a.balance,o.state,(SELECT count(*) FROM v3_billing.ledger_entries WHERE account_id=a.id AND kind='adjustment')
		    FROM v3_billing.accounts a CROSS JOIN v3_commerce.orders o WHERE a.owner_type='user' AND a.owner_id=1 AND a.kind='wallet' AND o.id=$1`, o.ID).
			Scan(&actual, &actualState, &count); err != nil {
			t.Fatal(err)
		}
		if actual != balance || actualState != state || count != reversals {
			t.Fatalf("refund balance=%d state=%s reversals=%d", actual, actualState, count)
		}
	}
	assert(8_000_000, "paid", 1)
	if err := s.ConfirmRefundTotal(ctx, "test", o.TradeNo, "partial-1", o.Currency, 500); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatalf("same event ID with changed amount: %v", err)
	}
	for _, amount := range []int64{0, o.AmountMinor + 1} {
		if err := s.ConfirmRefundTotal(ctx, "test", o.TradeNo, "invalid", o.Currency, amount); err == nil {
			t.Fatalf("invalid refund amount accepted: %d", amount)
		}
	}
	if err := s.ConfirmRefundTotal(ctx, "test", o.TradeNo, "wrong-currency", "cny", o.AmountMinor); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatalf("refund currency mismatch: %v", err)
	}
	for range 2 {
		if err := s.ConfirmRefund(ctx, "test", o.TradeNo, "full"); err != nil {
			t.Fatal(err)
		}
	}
	assert(0, "refunded", 2)
}
