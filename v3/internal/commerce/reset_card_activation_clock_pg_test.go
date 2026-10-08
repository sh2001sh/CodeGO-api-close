//go:build pgintegration

package commerce_test

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestResetCardActivationReceiptCanonicalUnderAdvancingClock(t *testing.T) {
	_, pool, _ := newService(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 3, 2, 3, 4, 123456789, time.UTC)
	var ticks atomic.Int64
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{fakePayment{}}, commerce.Config{
		Now: func() time.Time { return base.Add(time.Duration(ticks.Add(1)) * 17 * time.Microsecond) }, ReturnOrigins: []string{"https://site.test"}})
	legacy := monthlyPlan(t, s)
	if _, err := s.BindSubscription(ctx, 1, legacy.ID, "advancing-clock-old"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,earned_total,available_total) VALUES(1,1,1)`); err != nil {
		t.Fatal(err)
	}
	_, rule := configuredCards(t, s, legacy, 927000)
	q, err := s.QuoteResetCards(ctx, 1, rule.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	exchange, err := s.ConfirmResetCards(ctx, 1, q.QuoteID, "advancing-clock-exchange", true)
	if err != nil || len(exchange.Cards) != 1 {
		t.Fatalf("exchange %+v err=%v", exchange, err)
	}
	first, err := s.ActivateBoundCard(ctx, 1, exchange.Cards[0].ID, "advancing-clock-activate")
	if err != nil || first.ActivatedAt == nil || first.SubscriptionID == nil {
		t.Fatalf("first receipt %+v err=%v", first, err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		replay, err := s.ActivateBoundCard(ctx, 1, first.ID, "advancing-clock-activate")
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(replay)
		if err != nil || !bytes.Equal(payload, firstJSON) {
			t.Fatalf("activation receipt drift first=%s replay=%s err=%v", firstJSON, payload, err)
		}
	}
	var start, end time.Time
	if err = pool.QueryRow(ctx, `SELECT starts_at,expires_at FROM v3_commerce.subscriptions WHERE id=$1`, *first.SubscriptionID).Scan(&start, &end); err != nil || !start.Equal(*first.ActivatedAt) || !end.Equal(first.ActivatedAt.Add(90*24*time.Hour)) {
		t.Fatalf("activation duration start=%s end=%s receipt=%s err=%v", start, end, first.ActivatedAt, err)
	}
}
