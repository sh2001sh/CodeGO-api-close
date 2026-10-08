//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func configuredCards(t *testing.T, s *commerce.Service, source commerce.Plan, budget credits.Micro) (commerce.Plan, commerce.ResetCardRule) {
	t.Helper()
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "fixed card", PolicyVersion: commerce.PolicyStandardV2, PriceMinor: 1000, Currency: "usd", Credits: 1030000, DurationUnit: "day", DurationValue: 90, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := s.SaveRedesignRules(ctx, commerce.RedesignRules{CardRules: []commerce.ResetCardRule{{Name: "audited old tier card", ReferencePlanID: source.ID, CardPlanID: p.ID, Credits: 1030000, CostPerCard: 927000, BaselineCost: 900000, BudgetTotal: budget, IncrementalBudgetTotal: 1000000, Enabled: true, Reviewed: true}}})
	if err != nil {
		t.Fatal(err)
	}
	return p, rules.CardRules[0]
}

func TestResetCardsVoluntaryReplayBudgetAndIndependentDelayedActivation(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	legacy := monthlyPlan(t, s)
	o := create(t, s, legacy.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	original := onlySubscription(t, s)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,earned_total,available_total) VALUES(1,3,3)`); err != nil {
		t.Fatal(err)
	}
	p, rule := configuredCards(t, s, legacy, 2781000)
	q, err := s.QuoteResetCards(ctx, 1, rule.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := s.QuoteResetCards(ctx, 1, rule.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConfirmResetCards(ctx, 1, q.QuoteID, "card-denied", false); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("consent ignored %v", err)
	}
	if _, err = s.ConfirmResetCards(ctx, 2, q.QuoteID, "card-stolen", true); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("card ownership %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, e := s.ConfirmResetCards(ctx, 1, q.QuoteID, "cards-once", true)
			if e == nil && (result.Quantity != 2 || len(result.Cards) != 2 || result.RemainingCount != 1) {
				e = errors.New("wrong exchange")
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if _, err = s.ConfirmResetCards(ctx, 1, stale.QuoteID, "card-stale", true); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("stale count quote %v", err)
	}
	var earned, used, exchanged, available int64
	if err = pool.QueryRow(ctx, `SELECT earned_total,used_total,exchanged_total,available_total FROM v3_commerce.subscription_reset_opportunity_accounts WHERE user_id=1`).Scan(&earned, &used, &exchanged, &available); err != nil || earned != 3 || used != 0 || exchanged != 2 || available != 1 {
		t.Fatalf("opportunity accounting %d %d %d %d %v", earned, used, exchanged, available, err)
	}
	var cost, increment int64
	if err = pool.QueryRow(ctx, `SELECT budget_reserved,incremental_reserved FROM v3_commerce.reset_card_rules WHERE id=$1`, rule.ID).Scan(&cost, &increment); err != nil || cost != 1854000 || increment != 54000 {
		t.Fatalf("cost %d increment %d %v", cost, increment, err)
	}
	cards, err := s.BoundCards(ctx, 1)
	if err != nil || len(cards) != 2 {
		t.Fatalf("cards %+v %v", cards, err)
	}
	p.Credits = 2000000
	if _, err = s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(180 * 24 * time.Hour)
	card, err := s.ActivateBoundCard(ctx, 1, cards[0].ID, "card-activation-once")
	if err != nil || card.SubscriptionID == nil {
		t.Fatalf("activation %+v %v", card, err)
	}
	replay, err := s.ActivateBoundCard(ctx, 1, cards[0].ID, "card-activation-once")
	if err != nil || replay.SubscriptionID == nil || *replay.SubscriptionID != *card.SubscriptionID {
		t.Fatalf("activation replay %+v %v", replay, err)
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 2 {
		t.Fatalf("separate card grant %+v %v", subs, err)
	}
	fresh := subs[0]
	if fresh.TotalCredits != 1030000 || fresh.PolicyVersion != commerce.PolicyStandardV2 || fresh.ExpiresAt.Sub(*now) != 90*24*time.Hour || fresh.Balance != 1030000 || subs[1].ID != original.ID || subs[1].Balance != 1000 {
		t.Fatalf("frozen card grant %+v", subs)
	}
	if _, err = s.ActivateBoundCard(ctx, 1, cards[1].ID, "card-activation-once"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("activation key reused %v", err)
	}
	if _, err = s.ActivateBoundCard(ctx, 2, cards[1].ID, "foreign-card"); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign activation %v", err)
	}
	if err = s.ResetSubscription(ctx, fresh.ID, 1, "card-reset"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("card refreshed %v", err)
	}
	var income int64
	if err = pool.QueryRow(ctx, `SELECT recognized_revenue_credits FROM v3_commerce.subscriptions WHERE id=$1`, fresh.ID).Scan(&income); err != nil || income != 0 {
		t.Fatalf("card created income %d %v", income, err)
	}
}

func TestResetCardsBudgetCompetitionExpiryAndFailureLeaveCounts(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	legacy := monthlyPlan(t, s)
	for _, u := range []int64{1, 2} {
		if _, err := s.BindSubscription(ctx, u, legacy.ID, "reference-user-"+string(rune('0'+u))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,earned_total,available_total) VALUES(1,2,2),(2,2,2)`); err != nil {
		t.Fatal(err)
	}
	_, rule := configuredCards(t, s, legacy, 927000)
	q1, err := s.QuoteResetCards(ctx, 1, rule.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	q2, err := s.QuoteResetCards(ctx, 2, rule.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, q := range []commerce.ResetCardQuote{q1, q2} {
		wg.Add(1)
		go func(user int64, q commerce.ResetCardQuote) {
			defer wg.Done()
			_, err := s.ConfirmResetCards(ctx, user, q.QuoteID, "budget-user-"+string(rune('0'+user)), true)
			errs <- err
		}(int64(i+1), q)
	}
	wg.Wait()
	close(errs)
	ok, conflict := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else if errors.Is(e, commerce.ErrStateConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("budget outcomes=%d %d", ok, conflict)
	}
	var n, remaining int64
	if err = pool.QueryRow(ctx, `SELECT sum(exchanged_total)::bigint,sum(available_total)::bigint FROM v3_commerce.subscription_reset_opportunity_accounts`).Scan(&n, &remaining); err != nil || n != 1 || remaining != 3 {
		t.Fatalf("budget burned counts=%d remaining=%d %v", n, remaining, err)
	}
	rules, err := s.RedesignRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rule = rules.CardRules[0]
	rule.BudgetTotal = 2781000
	if _, err = s.SaveRedesignRules(ctx, commerce.RedesignRules{CardRules: []commerce.ResetCardRule{rule}}); err != nil {
		t.Fatal(err)
	}
	q, err := s.QuoteResetCards(ctx, 1, rule.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	*now = q.ExpiresAt
	if _, err = s.ConfirmResetCards(ctx, 1, q.QuoteID, "expired-card", true); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("expired card quote %v", err)
	}
	if _, err = s.QuoteResetCards(ctx, 1, rule.ID, 0); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("zero exchange %v", err)
	}
}
