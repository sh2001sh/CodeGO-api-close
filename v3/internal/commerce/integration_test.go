//go:build pgintegration

package commerce_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/migrations"
)

type fakePayment struct{}

func (fakePayment) Name() string { return "test" }
func (fakePayment) Checkout(_ context.Context, o commerce.Order, _, _ string) (commerce.Checkout, error) {
	return commerce.Checkout{Reference: "pay_" + o.TradeNo, URL: "https://checkout.test/" + o.TradeNo}, nil
}
func (fakePayment) Verify(_ context.Context, _ http.Header, body []byte) (commerce.PaymentEvent, error) {
	var e commerce.PaymentEvent
	err := json.Unmarshal(body, &e)
	return e, err
}

func isolatedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("commerce_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
		_ = admin.Close(ctx)
	})
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range names {
		sql, err := migrations.Read(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatalf("migration %s: %v", file, err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(1,'payer'),(2,'other')`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func newService(t *testing.T) (*commerce.Service, *pgxpool.Pool, *time.Time) {
	t.Helper()
	pool := isolatedPool(t)
	now := time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{fakePayment{}}, commerce.Config{
		Now: func() time.Time { return now }, ReturnOrigins: []string{"https://site.test"}})
	return s, pool, &now
}

func create(t *testing.T, s *commerce.Service, plan int64) commerce.Order {
	t.Helper()
	o, err := s.Create(context.Background(), commerce.CreateOrder{UserID: 1, AmountMinor: 1200, PlanID: plan, Provider: "test", SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func payment(o commerce.Order) commerce.PaymentEvent {
	return commerce.PaymentEvent{ID: "event_" + o.TradeNo, TradeNo: o.TradeNo, Reference: *o.ProviderReference, AmountMinor: o.AmountMinor, Currency: o.Currency, Paid: true}
}

func TestPaymentConcurrentCallbacksCreditOnceAndRejectMismatch(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	e := payment(o)
	bad := e
	bad.AmountMinor++
	if err := s.Fulfill(ctx, "test", bad); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
	var wg sync.WaitGroup
	fail := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); fail <- s.Fulfill(ctx, "test", e) }()
	}
	wg.Wait()
	close(fail)
	for err := range fail {
		if err != nil {
			t.Fatal(err)
		}
	}
	var balance, entries, receipts int64
	if err := pool.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries),(SELECT count(*) FROM v3_commerce.payment_events) FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`).Scan(&balance, &entries, &receipts); err != nil {
		t.Fatal(err)
	}
	if balance != 12_000_000 || entries != 1 || receipts != 1 {
		t.Fatalf("balance=%d entries=%d receipts=%d", balance, entries, receipts)
	}
	if _, err := s.GetOrder(ctx, 2, o.TradeNo); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("other user's order visible: %v", err)
	}
	if err := s.Cancel(ctx, 1, o.TradeNo); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("paid order cancellation: %v", err)
	}
}

func TestLateVerifiedPaymentFulfillsCanceledOrder(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Cancel(ctx, 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	result, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || result.State != "paid" {
		t.Fatalf("late payment state=%s err=%v", result.State, err)
	}
}

func TestSubscriptionGrantExpiresAndCannotRefundTwice(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "month", PriceMinor: 500, Currency: "usd", Credits: 8_000_000, PeriodSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 1 || subs[0].Balance != 8_000_000 {
		t.Fatalf("subs=%+v err=%v", subs, err)
	}
	*now = now.Add(time.Minute)
	for i := range 2 {
		n, err := s.ExpireSubscriptions(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1-i {
			t.Fatalf("expiry pass %d count=%d", i, n)
		}
	}
	var balance, entries int64
	if err = pool.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries WHERE account_id=$1) FROM v3_billing.accounts WHERE id=$1`, subs[0].AccountID).Scan(&balance, &entries); err != nil {
		t.Fatal(err)
	}
	if balance != 0 || entries != 2 {
		t.Fatalf("expired balance=%d entries=%d", balance, entries)
	}
	for range 2 {
		if err = s.ConfirmRefund(ctx, "test", o.TradeNo, "refund-1"); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || result.State != "refunded" {
		t.Fatalf("state=%s err=%v", result.State, err)
	}
}

func TestReceiptIDCannotBeReusedForAnotherOrder(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	first := create(t, s, 0)
	second := create(t, s, 0)
	e := payment(first)
	if err := s.Fulfill(ctx, "test", e); err != nil {
		t.Fatal(err)
	}
	secondEvent := payment(second)
	secondEvent.ID = e.ID
	if err := s.Fulfill(ctx, "test", secondEvent); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatalf("reused event: %v", err)
	}
	result, err := s.GetOrder(ctx, 1, second.TradeNo)
	if err != nil || result.State != "created" {
		t.Fatalf("second state=%s err=%v", result.State, err)
	}
}
