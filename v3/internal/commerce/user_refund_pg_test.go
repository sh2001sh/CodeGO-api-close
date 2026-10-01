//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type refundPaymentProvider struct{}

func (refundPaymentProvider) Name() string { return "epay" }
func (refundPaymentProvider) Checkout(_ context.Context, o commerce.Order, _, _ string) (commerce.Checkout, error) {
	return commerce.Checkout{Reference: o.TradeNo, URL: "https://checkout.test/" + o.TradeNo}, nil
}
func (refundPaymentProvider) Verify(context.Context, http.Header, []byte) (commerce.PaymentEvent, error) {
	return commerce.PaymentEvent{}, commerce.ErrInvalid
}

type refundMock struct {
	mu        sync.Mutex
	state     string
	ambiguous bool
	creates   int
	requests  map[string]commerce.RefundPayment
}

func (p *refundMock) CreateRefund(_ context.Context, in commerce.RefundPayment) (commerce.RefundProviderResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.requests == nil {
		p.requests = map[string]commerce.RefundPayment{}
	}
	p.creates++
	p.requests[in.RefundNo] = in
	if p.ambiguous {
		p.ambiguous = false
		return commerce.RefundProviderResult{}, errors.New("response lost after provider accepted")
	}
	return commerce.RefundProviderResult{RefundNo: in.RefundNo, RefundID: "remote-" + in.RefundNo, AmountMinor: in.AmountMinor, State: p.state}, nil
}
func (p *refundMock) QueryRefund(_ context.Context, _ string, no string) (commerce.RefundProviderResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	in, ok := p.requests[no]
	if !ok {
		return commerce.RefundProviderResult{State: "not_found"}, nil
	}
	return commerce.RefundProviderResult{RefundNo: no, RefundID: "remote-" + no, AmountMinor: in.AmountMinor, State: p.state}, nil
}

func refundServices(t *testing.T) (*commerce.Service, *commerce.UserRefunds, *pgxpool.Pool, *refundMock) {
	t.Helper()
	pool := isolatedPool(t)
	poster := ledger.NewPoster(pool)
	payments := commerce.New(pool, poster, []commerce.PaymentProvider{refundPaymentProvider{}}, commerce.Config{Currency: "cny", ReturnOrigins: []string{"https://site.test"}})
	provider := &refundMock{state: "success"}
	return payments, commerce.NewUserRefunds(pool, poster, nil, provider), pool, provider
}
func paidRefundOrder(t *testing.T, s *commerce.Service, amount, plan int64) commerce.Order {
	t.Helper()
	o, err := s.Create(context.Background(), commerce.CreateOrder{UserID: 1, AmountMinor: amount, PlanID: plan, Provider: "epay", SuccessURL: "https://site.test/ok", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Fulfill(context.Background(), "epay", commerce.PaymentEvent{ID: "provider-" + o.TradeNo, TradeNo: o.TradeNo, Reference: o.TradeNo, AmountMinor: o.AmountMinor, Currency: "cny", Paid: true})
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func refundAccount(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func refundPost(t *testing.T, pool *pgxpool.Pool, account int64, amount credits.Micro, kind, id string) {
	t.Helper()
	if _, err := ledger.NewPoster(pool).Post(context.Background(), billing.Entry{AccountID: account, Amount: amount, Kind: kind, OperationID: id}); err != nil {
		t.Fatal(err)
	}
}

func TestUserRefundProportionalFIFOConcurrentReplayAndOwnership(t *testing.T) {
	s, refunds, pool, provider := refundServices(t)
	ctx := context.Background()
	o := paidRefundOrder(t, s, 1000, 0)
	account := refundAccount(t, pool)
	refundPost(t, pool, account, -7_000_000, "usage", "spent")
	refundPost(t, pool, account, 9_000_000, "reward", "later-promo")
	paidRefundOrder(t, s, 500, 0)
	if _, err := refunds.Create(ctx, 2, commerce.UserRefundRequest{OrderType: "balance", TradeNo: o.TradeNo}); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign refund: %v", err)
	}
	items, err := refunds.Eligible(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[1].RemainingQuota != 3_000_000 || items[1].RefundAmountMinor != 294 {
		t.Fatalf("quotes: %+v", items)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			r, e := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: o.TradeNo})
			if e != nil && !errors.Is(e, commerce.ErrFundingPending) {
				t.Errorf("concurrent: %v", e)
			}
			if e == nil && (r.Status != "success" || r.AmountMinor != 294) {
				t.Errorf("refund: %+v", r)
			}
		})
	}
	wg.Wait()
	var balance, count int64
	if err = pool.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_commerce.user_refunds) FROM v3_billing.accounts WHERE id=$1`, account).Scan(&balance, &count); err != nil {
		t.Fatal(err)
	}
	if balance != 14_000_000 || count != 1 || provider.creates != 1 {
		t.Fatalf("balance=%d rows=%d creates=%d", balance, count, provider.creates)
	}
}

func TestUserRefundCannotRefundSpentTopupWithLaterFunds(t *testing.T) {
	s, refunds, pool, _ := refundServices(t)
	ctx := context.Background()
	o := paidRefundOrder(t, s, 1000, 0)
	account := refundAccount(t, pool)
	refundPost(t, pool, account, -10_000_000, "usage", "spent-all")
	paidRefundOrder(t, s, 2000, 0)
	refundPost(t, pool, account, 30_000_000, "reward", "promo")
	if _, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: o.TradeNo}); !errors.Is(err, commerce.ErrRefundUnavailable) {
		t.Fatalf("refunded used old topup: %v", err)
	}
}

func TestUserRefundAmbiguousResponseRetainsCreditsUntilConfirmed(t *testing.T) {
	s, refunds, pool, provider := refundServices(t)
	ctx := context.Background()
	o := paidRefundOrder(t, s, 1000, 0)
	provider.ambiguous = true
	r, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: o.TradeNo})
	if err != nil || r.Status != "processing" || r.Message == "" {
		t.Fatalf("ambiguity: %+v %v", r, err)
	}
	var balance int64
	if err = pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1`, refundAccount(t, pool)).Scan(&balance); err != nil || balance != 0 {
		t.Fatalf("reserved balance=%d %v", balance, err)
	}
	if _, err = refunds.Sync(ctx, 2, r.RefundNo); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign sync: %v", err)
	}
	for range 2 {
		r, err = refunds.Sync(ctx, 1, r.RefundNo)
		if err != nil || r.Status != "success" {
			t.Fatalf("sync: %+v %v", r, err)
		}
	}
	if provider.creates != 1 {
		t.Fatalf("duplicate remote refund: %d", provider.creates)
	}
}

func TestUserRefundDefinitiveFailureRestoresOriginalFIFOGrant(t *testing.T) {
	s, refunds, pool, provider := refundServices(t)
	ctx := context.Background()
	o := paidRefundOrder(t, s, 1000, 0)
	provider.state = "processing"
	r, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: o.TradeNo})
	if err != nil {
		t.Fatal(err)
	}
	paidRefundOrder(t, s, 500, 0)
	provider.state = "failed"
	r, err = refunds.Sync(ctx, 1, r.RefundNo)
	if err != nil || r.Status != "failed" {
		t.Fatalf("failed sync: %+v %v", r, err)
	}
	refundPost(t, pool, refundAccount(t, pool), -7_000_000, "usage", "after-failed-refund")
	items, err := refunds.Eligible(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[1].RemainingQuota != 3_000_000 || items[0].RemainingQuota != 5_000_000 {
		t.Fatalf("restore reordered FIFO: %+v", items)
	}
	provider.state = "success"
	retry, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: o.TradeNo})
	if err != nil || retry.Status != "success" || retry.RefundNo == r.RefundNo || retry.AmountMinor != 294 {
		t.Fatalf("retry: %+v %v", retry, err)
	}
}

func TestUserRefundSubscriptionCancelsFutureBenefitAndUsesLifetimeConsumption(t *testing.T) {
	s, refunds, pool, _ := refundServices(t)
	ctx := context.Background()
	plan, err := s.SavePlan(ctx, commerce.Plan{Name: "periodic", Currency: "cny", PriceMinor: 1000, Credits: 10_000_000, PeriodCredits: 4_000_000, PeriodSeconds: 3600, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := paidRefundOrder(t, s, 1000, plan.ID)
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	refundPost(t, pool, subs[0].AccountID, -1_000_000, "usage", "subscription-spend")
	r, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "subscription", TradeNo: o.TradeNo})
	if err != nil || r.AmountMinor != 882 || r.Status != "success" {
		t.Fatalf("subscription: %+v %v", r, err)
	}
	subs, err = s.ListSubscriptions(ctx, 1)
	if err != nil || subs[0].State != "canceled" || subs[0].Balance != 0 {
		t.Fatalf("subscription still usable: %+v %v", subs, err)
	}
}

func TestUserRefundImportedOpeningNeedsVerifiableFundingOrigin(t *testing.T) {
	s, refunds, pool, _ := refundServices(t)
	ctx := context.Background()
	o, err := s.Create(ctx, commerce.CreateOrder{UserID: 1, AmountMinor: 1000, Provider: "epay", SuccessURL: "https://site.test/ok", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	var account int64
	if err = pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',1,'wallet') RETURNING id`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	opening, err := ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: account, Amount: 10_000_000, Kind: "opening", OperationID: "migration:wallet:1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.orders SET state='paid',payment_event_id='imported-remote-order' WHERE id=$1`, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: o.TradeNo}); !errors.Is(err, commerce.ErrRefundUnavailable) {
		t.Fatalf("anonymous opening refunded: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_commerce.user_refund_origins(order_id,account_id,original_credits,remaining_credits,ledger_cursor) VALUES($1,$2,10000000,3000000,$3)`, o.ID, account, opening.EntryID); err != nil {
		t.Fatal(err)
	}
	items, err := refunds.Eligible(ctx, 1)
	if err != nil || len(items) != 1 || items[0].RemainingQuota != 3_000_000 || items[0].RefundAmountMinor != 294 {
		t.Fatalf("origin attribution: %+v %v", items, err)
	}
	refundPost(t, pool, account, -4_000_000, "usage", "post-migration-spend")
	refundPost(t, pool, account, 9_000_000, "reward", "post-migration-reward")
	if _, err = refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: o.TradeNo}); !errors.Is(err, commerce.ErrRefundUnavailable) {
		t.Fatalf("spent imported grant reappeared: %v", err)
	}
}
