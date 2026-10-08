//go:build pgintegration

package commerce_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestPlanRetirementPreservesIssuedAndImportedUnclaimedCodes(t *testing.T) {
	for _, imported := range []bool{false, true} {
		t.Run(map[bool]string{false: "frozen code", true: "imported code"}[imported], func(t *testing.T) {
			s, pool, _ := newService(t)
			ctx := context.Background()
			p := packagePlan(t, s, 1000, 1000)
			key := "legacy-code-retired-plan"
			if imported {
				digest := sha256.Sum256([]byte(key))
				if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.redemption_codes(code_hash,name,credits,redeem_type,plan_id,plan_title) VALUES($1,'imported',0,'subscription',$2,$3)`, digest[:], p.ID, p.Name); err != nil {
					t.Fatal(err)
				}
			} else {
				code, err := s.IssueTypedRedemption(ctx, commerce.IssueRedemptionInput{Name: "issued", RedeemType: "subscription", PlanID: p.ID})
				if err != nil {
					t.Fatal(err)
				}
				key = code.Key
			}
			if err := s.DeletePlan(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
			plans, err := s.ListPlans(ctx, true)
			if err != nil || len(plans) != 1 || plans[0].Enabled {
				t.Fatalf("retired plan=%+v err=%v", plans, err)
			}
			if _, err = s.Create(ctx, commerce.CreateOrder{UserID: 1, PlanID: p.ID, Provider: "test", SuccessURL: "https://site.test/s", CancelURL: "https://site.test/c"}); !errors.Is(err, commerce.ErrNotFound) {
				t.Fatalf("retired plan still sold: %v", err)
			}
			result, err := s.RedeemTyped(ctx, 1, key)
			if err != nil || result.UserSubscriptionID <= 0 || result.PlanID != p.ID {
				t.Fatalf("issued obligation not fulfilled: %+v err=%v", result, err)
			}
			replay, err := s.RedeemTyped(ctx, 1, key)
			if err != nil || replay != result {
				t.Fatalf("redemption replay=%+v err=%v", replay, err)
			}
		})
	}
}
