//go:build pgintegration

package commerce_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestTypedRedemptionImportedSubscriptionCreatesOneFreshPackageAndStableReceipt(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "redeem", PriceMinor: 1000, Currency: "usd", Credits: 5000, PeriodCredits: 1000, PeriodSeconds: 86400, ResetPeriod: "daily", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.plans SET model_limits='{"model-a":700}' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	// Codes create a separate package; blind-box reward merging is a different rule.
	if _, err = s.BindSubscription(ctx, 1, p.ID, "existing-package"); err != nil {
		t.Fatal(err)
	}
	// A source-imported subscription code legitimately has no monetary credit.
	key := "legacy-native-subscription-code"
	digest := sha256.Sum256([]byte(key))
	if _, err = pool.Exec(ctx, `INSERT INTO v3_commerce.redemption_codes(code_hash,name,credits,redeem_type,plan_id,plan_title)
	 VALUES($1,'imported',0,'subscription',$2,'source title')`, digest[:], p.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan commerce.RedemptionResult, 12)
	fail := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := s.RedeemTyped(ctx, 1, key); results <- r; fail <- e }()
	}
	wg.Wait()
	close(results)
	close(fail)
	for e := range fail {
		if e != nil {
			t.Fatal(e)
		}
	}
	var prior commerce.RedemptionResult
	for r := range results {
		if r.RedeemType != "subscription" || r.Credits != 0 || r.PlanID != p.ID || r.PlanTitle != "redeem" || r.UserSubscriptionID <= 0 {
			t.Fatalf("result=%+v", r)
		}
		if prior.UserSubscriptionID != 0 && prior != r {
			t.Fatalf("replay=%+v prior=%+v", r, prior)
		}
		prior = r
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.subscriptions WHERE user_id=1`); n != 2 {
		t.Fatalf("subscriptions=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='subscription_grant'`); n != 2 {
		t.Fatalf("entries=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_billing.accounts WHERE owner_type='user' AND kind='wallet'`); n != 0 {
		t.Fatalf("wallet credited=%d", n)
	}
	var source, limits string
	var balance int64
	if err = pool.QueryRow(ctx, `SELECT s.source,s.model_limits::text,a.balance FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id WHERE s.id=$1`, prior.UserSubscriptionID).Scan(&source, &limits, &balance); err != nil {
		t.Fatal(err)
	}
	if source != "redemption" || limits != "{\"model-a\": 700}" || balance != 1000 {
		t.Fatalf("source=%s limits=%s balance=%d", source, limits, balance)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.plans SET name='edited',credits=99999 WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	replay, err := s.RedeemTyped(ctx, 1, key)
	if err != nil || replay != prior {
		t.Fatalf("edited replay=%+v err=%v", replay, err)
	}
	if _, err = s.RedeemTyped(ctx, 2, key); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("other user=%v", err)
	}
}

func TestTypedRedemptionSubscriptionCapAndDeletedCodesRemainUnclaimed(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "cap", PriceMinor: 100, Currency: "usd", Credits: 5000, PeriodSeconds: 86400, Enabled: true, MaxPurchasePerUser: 1})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.IssueTypedRedemption(ctx, commerce.IssueRedemptionInput{Name: "first", RedeemType: "subscription", PlanID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.IssueTypedRedemption(ctx, commerce.IssueRedemptionInput{Name: "second", RedeemType: "subscription", PlanID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemTyped(ctx, 1, first.Key); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemTyped(ctx, 1, second.Key); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("cap=%v", err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.redemption_codes WHERE id=$1 AND state='active' AND redeem_result IS NULL`, second.ID); n != 1 {
		t.Fatal("cap consumed code")
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.redemption_codes SET deleted_at=now() WHERE id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemTyped(ctx, 2, second.Key); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("deleted=%v", err)
	}
	list, err := s.ListRedemptions(ctx, 0, 100)
	if err != nil || len(list) != 1 || list[0].RedeemType != "subscription" || list[0].Key != "" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}

func TestTypedRedemptionRejectsInconsistentBenefits(t *testing.T) {
	s, _, _ := newService(t)
	for _, in := range []commerce.IssueRedemptionInput{
		{RedeemType: "credits"}, {RedeemType: "credits", Credits: 10, PlanID: 1}, {RedeemType: "subscription", PlanID: 1, Credits: 1},
		{RedeemType: "blind_box", BlindBoxQuantity: 101}, {RedeemType: "blind_box", BlindBoxQuantity: 1, Credits: 1}, {RedeemType: "old_points", Credits: 100},
	} {
		if _, err := s.IssueTypedRedemption(context.Background(), in); !errors.Is(err, commerce.ErrInvalid) {
			t.Fatalf("input=%+v err=%v", in, err)
		}
	}
}

func TestTypedRedemptionImportedUsedCodeNeverReissuesBenefits(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	key := "legacy-used-box-code"
	digest := sha256.Sum256([]byte(key))
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.redemption_codes(code_hash,name,credits,state,claimed_by,claimed_at,redeem_type,blind_box_quantity)
	 VALUES($1,'already claimed',0,'used',1,now(),'blind_box',4)`, digest[:]); err != nil {
		t.Fatal(err)
	}
	r, err := s.RedeemTyped(ctx, 1, key)
	if err != nil || r.RedeemType != "blind_box" || r.BlindBoxQuantity != 4 || r.BlindBoxOrderID != 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_billing.ledger_entries`); n != 0 {
		t.Fatalf("ledger=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_orders`); n != 0 {
		t.Fatalf("orders=%d", n)
	}
}
