//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func checkoutCampaignSetting(t *testing.T, pool *pgxpool.Pool, now time.Time, factor string) {
	t.Helper()
	for key, value := range map[string]string{"enabled": "true", "multiplier": factor, "start_at": fmt.Sprint(now.Unix()), "end_at": fmt.Sprint(now.Add(time.Hour).Unix())} {
		if _, err := pool.Exec(context.Background(), `INSERT INTO v3_platform.settings(key,value) VALUES($1,$2::jsonb) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, "payment_setting.first_purchase_discount_"+key, value); err != nil {
			t.Fatal(err)
		}
	}
}

func checkoutMonthly(t *testing.T, s *commerce.Service, amount int64) commerce.Plan {
	t.Helper()
	p, err := s.SavePlan(context.Background(), commerce.Plan{Name: "monthly discount", PriceMinor: amount, Currency: "usd", Credits: 1000000, Enabled: true, PlanType: "monthly", DurationUnit: "month", DurationValue: 1, ResetPeriod: "never"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func checkoutCard(t *testing.T, pool *pgxpool.Pool, kind string, rate int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO v3_marketplace.blind_box_props(user_id,kind,title,discount_rate_ppm) VALUES(1,$1,'coupon',$2) RETURNING id`, kind, rate).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCheckoutDiscountCampaignSerializesClaimsAndSuppressesCoupons(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	checkoutCampaignSetting(t, pool, *now, "0.80000001")
	p := checkoutMonthly(t, s, 10001)
	card := checkoutCard(t, pool, "subscription_discount", 100000)
	var wg sync.WaitGroup
	orders := make(chan commerce.Order, 8)
	fails := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := s.Create(ctx, commerce.CreateOrder{UserID: 1, PlanID: p.ID, Provider: "test", SuccessURL: "https://site.test/s", CancelURL: "https://site.test/c"})
			if err != nil {
				fails <- err
				return
			}
			orders <- o
		}()
	}
	wg.Wait()
	close(orders)
	close(fails)
	for err := range fails {
		t.Fatal(err)
	}
	var campaign, coupon int
	var chosen commerce.Order
	for o := range orders {
		d, err := s.GetCheckoutDiscount(ctx, 1, o.TradeNo)
		if errors.Is(err, commerce.ErrNotFound) {
			if o.AmountMinor != 10001 {
				t.Fatal(o)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if d.Campaign {
			campaign++
			chosen = o
			if o.AmountMinor != 8001 || d.PropID != nil {
				t.Fatal(d)
			}
		} else {
			coupon++
			if d.PropID == nil || *d.PropID != card || o.AmountMinor != 9001 {
				t.Fatal(d)
			}
		}
		if o.Credits != p.Credits {
			t.Fatal("discount changed grant")
		}
	}
	if campaign != 1 || coupon != 1 {
		t.Fatalf("campaign=%d coupon=%d", campaign, coupon)
	}
	checkoutCampaignSetting(t, pool, *now, "0.1")
	bad := payment(chosen)
	bad.AmountMinor++
	if err := s.Fulfill(ctx, "test", bad); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatal(err)
	}
	if err := s.Fulfill(ctx, "test", payment(chosen)); err != nil {
		t.Fatal(err)
	}
	if err := s.Fulfill(ctx, "test", payment(chosen)); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetCheckoutDiscount(ctx, 1, chosen.TradeNo)
	if err != nil || d.State != "consumed" || d.Multiplier != "0.80000001" || d.PaidMinor != 8001 {
		t.Fatalf("frozen=%+v err=%v", d, err)
	}
	if _, err = s.GetCheckoutDiscount(ctx, 2, chosen.TradeNo); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCheckoutDiscountCouponPaymentReplayReleaseReuseAndLateReview(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	card := checkoutCard(t, pool, "topup_discount", 100000)
	o := create(t, s, 0)
	if o.AmountMinor != 1080 || o.Credits != 12000000 {
		t.Fatalf("order=%+v", o)
	}
	bad := payment(o)
	bad.AmountMinor++
	if err := s.Fulfill(ctx, "test", bad); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatal(err)
	}
	if err := s.Cancel(ctx, 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecoverCheckoutDiscounts(ctx, 100); err != nil {
		t.Fatal(err)
	}
	next := create(t, s, 0)
	if next.AmountMinor != 1080 {
		t.Fatalf("released coupon unavailable %+v", next)
	}
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	late, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || late.State != "paid" || late.FulfillmentState != "requires_review" {
		t.Fatalf("late=%+v err=%v", late, err)
	}
	var entries int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries`).Scan(&entries); err != nil || entries != 0 {
		t.Fatalf("late grant entries=%d err=%v", entries, err)
	}
	if err = s.ConfirmRefund(ctx, "test", o.TradeNo, "late_refund"); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries`).Scan(&entries); err != nil || entries != 0 {
		t.Fatalf("ungranted refund debited entries=%d err=%v", entries, err)
	}
	var wg sync.WaitGroup
	fail := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); fail <- s.Fulfill(ctx, "test", payment(next)) }()
	}
	wg.Wait()
	close(fail)
	for err := range fail {
		if err != nil {
			t.Fatal(err)
		}
	}
	var status, trade string
	if err = pool.QueryRow(ctx, `SELECT status,reserved_order_trade_no FROM v3_marketplace.blind_box_props WHERE id=$1`, card).Scan(&status, &trade); err != nil || status != "used" || trade != next.TradeNo {
		t.Fatalf("status=%s trade=%s err=%v", status, trade, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries`).Scan(&entries); err != nil || entries != 1 {
		t.Fatalf("replay entries=%d err=%v", entries, err)
	}
}

func TestCheckoutDiscountCampaignWindowAndPlanEligibility(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	var err error
	checkoutCampaignSetting(t, pool, *now, "0.8")
	p := checkoutMonthly(t, s, 100)
	*now = now.Add(-time.Second)
	before := create(t, s, p.ID)
	if before.AmountMinor != 100 {
		t.Fatal(before)
	}
	*now = now.Add(time.Second)
	start := create(t, s, p.ID)
	if start.AmountMinor != 80 {
		t.Fatal(start)
	}
	if err := s.Cancel(ctx, 1, start.TradeNo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecoverCheckoutDiscounts(ctx, 100); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	end := create(t, s, p.ID)
	if end.AmountMinor != 80 {
		t.Fatal(end)
	}
	if err := s.Cancel(ctx, 1, end.TradeNo); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	after := create(t, s, p.ID)
	if after.AmountMinor != 100 {
		t.Fatal(after)
	}
	if err := s.Fulfill(ctx, "test", payment(after)); err != nil {
		t.Fatal(err)
	}
	checkoutCampaignSetting(t, pool, *now, "0.8")
	prior := create(t, s, p.ID)
	if prior.AmountMinor != 100 {
		t.Fatal("paid monthly did not block campaign")
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.plans SET plan_type='starter' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	p = checkoutMonthly(t, s, 100)
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.plans SET duration_unit='day' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	nonmonth := create(t, s, p.ID)
	if nonmonth.AmountMinor != 100 {
		t.Fatal("non-month duration eligible")
	}
}
