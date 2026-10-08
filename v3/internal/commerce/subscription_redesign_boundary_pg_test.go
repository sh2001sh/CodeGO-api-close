//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestWholeWalletConversionChangedUsageFailsRestoresAndRequotes(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := monthlyPlan(t, s)
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	q := configureConversion(t, s, sub.ID, 1000, 1000000, 1000000, nil)
	if _, err := ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -100, Kind: "usage", OperationID: "drift:late-usage"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "drift-confirm", true)
	if !errors.Is(err, commerce.ErrStateConflict) || c.State != "failed" {
		t.Fatalf("stale conversion %+v %v", c, err)
	}
	after := onlySubscription(t, s)
	if after.State != "active" || after.ConvertedAt != nil || after.Balance != 900 || after.UsedCredits != 100 || after.AccountID == sub.AccountID {
		t.Fatalf("failed conversion lost rights %+v", after)
	}
	q, err = s.QuoteWalletConversion(ctx, 1, sub.ID)
	if err != nil || q.State != "quoted" || q.TargetCredits != 900000 {
		t.Fatalf("new quote %+v %v", q, err)
	}
	c, err = s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "drift-requoted", true)
	if err != nil || c.TargetCredits != 900000 {
		t.Fatalf("requote completion %+v %v", c, err)
	}
}

func TestWholeWalletConversionReviewedRefreshAndBenefitExpiry(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES('default'),('vip') ON CONFLICT DO NOTHING; UPDATE v3_identity.users SET group_name='default' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	p := monthlyPlan(t, s)
	p.UpgradeGroup = "vip"
	if _, err := s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	if _, err := ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -500, Kind: "usage", OperationID: "refresh:old-spent"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetSubscription(ctx, sub.ID, 1, "legacy-reviewed-reset"); err != nil {
		t.Fatal(err)
	}
	reviewed := credits.Micro(0)
	q := configureConversion(t, s, sub.ID, 1000, 1000000, 1000000, &reviewed)
	if q.State != "quoted" || q.PaidCredits != 0 || q.RewardCredits != 1000000 {
		t.Fatalf("refreshed review %+v", q)
	}
	if _, err := s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "reviewed-reset-converted", true); err != nil {
		t.Fatal(err)
	}
	var group string
	if err := pool.QueryRow(ctx, `SELECT group_name FROM v3_identity.users WHERE id=1`).Scan(&group); err != nil || group != "vip" {
		t.Fatalf("paid benefit removed early %q %v", group, err)
	}
	*now = sub.ExpiresAt.Add(-time.Nanosecond)
	if n, err := s.ExpireSubscriptions(ctx, 100); err != nil || n != 0 {
		t.Fatalf("benefit expired early n=%d %v", n, err)
	}
	*now = sub.ExpiresAt
	if n, err := s.ExpireSubscriptions(ctx, 100); err != nil || n != 1 {
		t.Fatalf("benefit expiry n=%d %v", n, err)
	}
	if err := pool.QueryRow(ctx, `SELECT group_name FROM v3_identity.users WHERE id=1`).Scan(&group); err != nil || group != "default" {
		t.Fatalf("benefit not restored %q %v", group, err)
	}
	if n, err := s.ExpireSubscriptions(ctx, 100); err != nil || n != 0 {
		t.Fatalf("benefit expiry replay n=%d %v", n, err)
	}
}

func TestWholeWalletConversionApprovedPendingUsesFrozenRule(t *testing.T) {
	_, pool, now := newService(t)
	ctx := context.Background()
	drain := &conversionDrain{}
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{fakePayment{}}, commerce.Config{Now: func() time.Time { return *now }, FundingDrain: drain, ReturnOrigins: []string{"https://site.test"}})
	p := monthlyPlan(t, s)
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	q := configureConversion(t, s, sub.ID, 1000, 1000000, 1000000, nil)
	if _, err := s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "frozen-pending", true); !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatal(err)
	}
	rules, err := s.RedesignRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r := rules.ConversionRules[0]
	r.WalletCredits = 2000000
	r.Enabled = false
	if _, err = s.SaveRedesignRules(ctx, commerce.RedesignRules{ConversionRules: []commerce.ConversionRule{r}}); err != nil {
		t.Fatal(err)
	}
	drain.ready = true
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 1 {
		t.Fatalf("frozen recovery=%d %v", n, err)
	}
	c, err := s.WalletConversion(ctx, 1, "frozen-pending")
	if err != nil || c.State != "completed" || c.TargetCredits != 1000000 {
		t.Fatalf("admin changed accepted quote %+v %v", c, err)
	}
}
