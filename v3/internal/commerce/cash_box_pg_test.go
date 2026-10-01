//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

func cashBoxFixture(t *testing.T) (*commerce.Service, *marketplace.Service, *pgxpool.Pool, marketplace.Pool) {
	t.Helper()
	pool := isolatedPool(t)
	now := func() time.Time { return time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC) }
	poster := ledger.NewPoster(pool)
	s := commerce.New(pool, poster, []commerce.PaymentProvider{commerce.NewEpay(commerce.EpayConfig{
		MerchantID: "merchant", Secret: "test-only-secret", BaseURL: "https://cashier.test", NotifyURL: "https://site.test/api/blind-box/epay/notify"})},
		commerce.Config{Now: now, ReturnOrigins: []string{"https://site.test"}, TopupCreditsPerMinor: 999999})
	m := marketplace.New(pool, poster, ledger.NewAccounts(pool), s, s, marketplace.Config{Now: now, Draw: func(int64) (int64, error) { return 0, nil }})
	s.SetCashBoxMarket(m)
	p, err := m.SavePool(context.Background(), marketplace.Pool{Name: "cash", Scope: "standard", Enabled: true,
		Price: 2500000, DailyLimit: 100, Rewards: []marketplace.Reward{{Kind: "credits", Title: "reward", Weight: 1, Amount: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	return s, m, pool, p
}

func cashBoxCreate(t *testing.T, s *commerce.Service, id int64, count int) commerce.Order {
	t.Helper()
	o, err := s.CreateCashBox(context.Background(), commerce.CreateCashBox{UserID: 1, PoolID: id, Quantity: count,
		Provider: "epay", PaymentMethod: "wxpay", SuccessURL: "https://site.test/blind-box?pay=pending", CancelURL: "https://site.test/blind-box?pay=fail"})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func cashBoxCount(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestCashBoxPaymentsConcurrentVerifiedCallbacksGrantInventoryWithoutWallet(t *testing.T) {
	s, _, pool, p := cashBoxFixture(t)
	o := cashBoxCreate(t, s, p.ID, 3)
	if o.AmountMinor != 750 || o.Credits != 0 || o.Kind != "blind_box" || o.Currency != "cny" || o.Selection.PaymentMethod != "wxpay" {
		t.Fatalf("incorrect cash snapshot: %+v", o)
	}
	if _, err := s.CashBoxOrder(context.Background(), 2, o.TradeNo); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("cross-user order leaked: %v", err)
	}
	e := payment(o)
	bad := e
	bad.AmountMinor++
	if err := s.Fulfill(context.Background(), "epay", bad); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatalf("altered amount accepted: %v", err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.payment_events`); n != 0 {
		t.Fatalf("mismatched amount left receipt: %d", n)
	}
	var wg sync.WaitGroup
	fail := make(chan error, 12)
	for range 12 {
		wg.Go(func() { fail <- s.Fulfill(context.Background(), "epay", e) })
	}
	wg.Wait()
	close(fail)
	for err := range fail {
		if err != nil {
			t.Fatal(err)
		}
	}
	for sql, want := range map[string]int64{
		`SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`: 3,
		`SELECT count(*) FROM v3_marketplace.blind_box_purchases`:                      1,
		`SELECT count(*) FROM v3_commerce.payment_events`:                              1,
		`SELECT count(*) FROM v3_billing.ledger_entries`:                               0,
		`SELECT count(*) FROM v3_billing.accounts`:                                     0,
	} {
		if got := cashBoxCount(t, pool, sql); got != want {
			t.Fatalf("%s: got=%d want=%d", sql, got, want)
		}
	}
	if err := s.ConfirmRefundTotal(context.Background(), "epay", o.TradeNo, "partial", "cny", 250); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatalf("partial box refund accepted: %v", err)
	}
	for range 2 {
		if err := s.ConfirmRefund(context.Background(), "epay", o.TradeNo, "full-refund"); err != nil {
			t.Fatal(err)
		}
	}
	if got := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='revoked'`); got != 3 {
		t.Fatalf("refund left live inventory: %d", got)
	}
	if err := s.Fulfill(context.Background(), "epay", e); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("refunded order resurrected: %v", err)
	}
}

func TestCashBoxRefundRejectsOpenedOrGiftedInventoryAtomically(t *testing.T) {
	for _, action := range []string{"opened", "gifted"} {
		t.Run(action, func(t *testing.T) {
			s, m, pool, p := cashBoxFixture(t)
			o := cashBoxCreate(t, s, p.ID, 2)
			if err := s.Fulfill(context.Background(), "epay", payment(o)); err != nil {
				t.Fatal(err)
			}
			if action == "opened" {
				if _, err := m.OpenBoxes(context.Background(), 1, "real-open", 1); err != nil {
					t.Fatal(err)
				}
			} else if _, err := m.GiftBoxes(context.Background(), 1, 2, "real-gift", 1); err != nil {
				t.Fatal(err)
			}
			if err := s.ConfirmRefund(context.Background(), "epay", o.TradeNo, "bad-refund"); !errors.Is(err, commerce.ErrStateConflict) {
				t.Fatalf("used inventory refunded: %v", err)
			}
			if got := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.payment_events WHERE event_id='bad-refund'`); got != 0 {
				t.Fatalf("failed refund left receipt: %d", got)
			}
			if got := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.orders WHERE state='paid'`); got != 1 {
				t.Fatalf("failed refund changed paid state: %d", got)
			}
			if got := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='revoked'`); got != 0 {
				t.Fatalf("failed refund revoked inventory: %d", got)
			}
		})
	}
}

type failingCashBoxMarket struct {
	*marketplace.Service
	err error
}

func (m failingCashBoxMarket) CompleteBoxOrderTx(ctx context.Context, tx pgx.Tx, user int64, trade string, amount int64, currency string) (marketplace.Purchase, error) {
	p, err := m.Service.CompleteBoxOrderTx(ctx, tx, user, trade, amount, currency)
	if err != nil {
		return p, err
	}
	return p, m.err
}

func TestCashBoxFulfillmentRollbackIncludesPaidStateReceiptAndInventory(t *testing.T) {
	s, m, pool, p := cashBoxFixture(t)
	o := cashBoxCreate(t, s, p.ID, 2)
	injected := errors.New("transaction failure after inventory")
	s.SetCashBoxMarket(failingCashBoxMarket{Service: m, err: injected})
	if err := s.Fulfill(context.Background(), "epay", payment(o)); !errors.Is(err, injected) {
		t.Fatalf("injected failure missing: %v", err)
	}
	for _, sql := range []string{`SELECT count(*) FROM v3_commerce.payment_events`, `SELECT count(*) FROM v3_marketplace.blind_box_items`, `SELECT count(*) FROM v3_marketplace.blind_box_purchases`} {
		if n := cashBoxCount(t, pool, sql); n != 0 {
			t.Fatalf("rollback left %d rows: %s", n, sql)
		}
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.orders WHERE state='created'`); n != 1 {
		t.Fatalf("rollback changed order state: %d", n)
	}
	s.SetCashBoxMarket(m)
	if err := s.Fulfill(context.Background(), "epay", payment(o)); err != nil {
		t.Fatal(err)
	}
}
