//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestSubscriptionGroupsPlanValidationAndNativePersistence(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := subscriptionGroupPlan(t, s, pool, "vip")
	p.UpgradeGroup = " vip "
	p.ModelLimits = map[string]int64{" chat ": 700, "unlimited": 0}
	p, err := s.SavePlan(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := s.ListPlans(ctx, true)
	if err != nil || len(plans) != 1 || plans[0].UpgradeGroup != "vip" || len(plans[0].ModelLimits) != 1 || plans[0].ModelLimits["chat"] != 700 {
		t.Fatalf("native plan policy=%+v err=%v", plans, err)
	}
	p.UpgradeGroup = "missing"
	if _, err = s.SavePlan(ctx, p); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("nonexistent plan group admitted: %v", err)
	}
	p.UpgradeGroup = "vip"
	p.ModelLimits = map[string]int64{"chat": -1}
	if _, err = s.SavePlan(ctx, p); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("negative model limit admitted: %v", err)
	}
	plans, err = s.ListPlans(ctx, true)
	if err != nil || plans[0].UpgradeGroup != "vip" || plans[0].ModelLimits["chat"] != 700 {
		t.Fatalf("rejected admin edit changed live policy=%+v err=%v", plans, err)
	}
}

func TestSubscriptionGroupsAdministrativeAndRedeemedGrants(t *testing.T) {
	for _, mode := range []string{"admin", "redemption"} {
		t.Run(mode, func(t *testing.T) {
			s, pool, _ := newService(t)
			ctx := context.Background()
			p := subscriptionGroupPlan(t, s, pool, "vip")
			var id int64
			var err error
			if mode == "admin" {
				id, err = s.BindSubscription(ctx, 1, p.ID, "group-admin")
				if err != nil {
					t.Fatal(err)
				}
				if replay, replayErr := s.BindSubscription(ctx, 1, p.ID, "group-admin"); replayErr != nil || replay != id {
					t.Fatalf("admin grant replay=%d original=%d err=%v", replay, id, replayErr)
				}
			} else {
				code, issueErr := s.IssueTypedRedemption(ctx, commerce.IssueRedemptionInput{Name: "group-plan", RedeemType: "subscription", PlanID: p.ID})
				if issueErr != nil {
					t.Fatal(issueErr)
				}
				result, redeemErr := s.RedeemTyped(ctx, 1, code.Key)
				if redeemErr != nil {
					t.Fatal(redeemErr)
				}
				id = result.UserSubscriptionID
				if replay, replayErr := s.RedeemTyped(ctx, 1, code.Key); replayErr != nil || replay.UserSubscriptionID != id {
					t.Fatalf("redemption replay=%+v original=%d err=%v", replay, id, replayErr)
				}
			}
			assertSubscriptionGroup(t, pool, id, "vip", "vip", "default")
			if err = s.EndSubscription(ctx, id, 1, false); err != nil {
				t.Fatal(err)
			}
			assertSubscriptionGroup(t, pool, id, "default", "vip", "default")
		})
	}
}
