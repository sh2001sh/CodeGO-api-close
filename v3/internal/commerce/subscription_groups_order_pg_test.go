//go:build pgintegration

package commerce_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSubscriptionGroupsEndOrderRestoresBaseAndRemainingEntitlement(t *testing.T) {
	for _, groups := range [][2]string{{"vip", "vip"}, {"vip", "premium"}} {
		for _, fifo := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s_%s_fifo_%t", groups[0], groups[1], fifo), func(t *testing.T) {
				s, pool, _ := newService(t)
				ctx := context.Background()
				firstPlan := subscriptionGroupPlan(t, s, pool, groups[0])
				first, err := s.BindSubscription(ctx, 1, firstPlan.ID, "first")
				if err != nil {
					t.Fatal(err)
				}
				secondPlan := subscriptionGroupPlan(t, s, pool, groups[1])
				second, err := s.BindSubscription(ctx, 1, secondPlan.ID, "second")
				if err != nil {
					t.Fatal(err)
				}
				order, remaining := []int64{first, second}, groups[1]
				if !fifo {
					order, remaining = []int64{second, first}, groups[0]
				}
				if err = s.EndSubscription(ctx, order[0], 1, false); err != nil {
					t.Fatal(err)
				}
				assertUserGroup(t, pool, remaining)
				if err = s.EndSubscription(ctx, order[1], 1, false); err != nil {
					t.Fatal(err)
				}
				assertUserGroup(t, pool, "default")
			})
		}
	}
}

func TestSubscriptionGroupsOldOverlappingSnapshotsPropagateBase(t *testing.T) {
	for _, secondGroup := range []string{"vip", "premium"} {
		t.Run(secondGroup, func(t *testing.T) {
			s, pool, _ := newService(t)
			ctx := context.Background()
			p := subscriptionGroupPlan(t, s, pool, "vip")
			first, err := s.BindSubscription(ctx, 1, p.ID, "legacy-first")
			if err != nil {
				t.Fatal(err)
			}
			p = subscriptionGroupPlan(t, s, pool, secondGroup)
			second, err := s.BindSubscription(ctx, 1, p.ID, "legacy-second")
			if err != nil {
				t.Fatal(err)
			}
			previous := "vip"
			if secondGroup == "vip" {
				previous = ""
			}
			if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET prev_user_group=$2 WHERE id=$1`, second, previous); err != nil {
				t.Fatal(err)
			}
			if err = s.EndSubscription(ctx, first, 1, false); err != nil {
				t.Fatal(err)
			}
			assertUserGroup(t, pool, secondGroup)
			if err = s.EndSubscription(ctx, second, 1, false); err != nil {
				t.Fatal(err)
			}
			assertUserGroup(t, pool, "default")
		})
	}
}

func TestSubscriptionGroupsBatchExpiryAndAdminOverride(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprintf("manual_%t", override), func(t *testing.T) {
			s, pool, now := newService(t)
			ctx := context.Background()
			for i, group := range []string{"vip", "premium", "vip"} {
				p := subscriptionGroupPlan(t, s, pool, group)
				if _, err := s.BindSubscription(ctx, 1, p.ID, fmt.Sprintf("overlap-%d", i)); err != nil {
					t.Fatal(err)
				}
			}
			if override {
				if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET group_name='manual' WHERE id=1`); err != nil {
					t.Fatal(err)
				}
			}
			*now = now.Add(365 * 24 * time.Hour)
			if n, err := s.ExpireSubscriptions(ctx, 100); err != nil || n != 3 {
				t.Fatalf("batch expiry=%d err=%v", n, err)
			}
			group := "default"
			if override {
				group = "manual"
			}
			assertUserGroup(t, pool, group)
			if n, err := s.ExpireSubscriptions(ctx, 100); err != nil || n != 0 {
				t.Fatalf("expiry replay=%d err=%v", n, err)
			}
			assertUserGroup(t, pool, group)
		})
	}
}

func TestSubscriptionGroupsResolvePreexistingEndedPredecessor(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := subscriptionGroupPlan(t, s, pool, "vip")
	first, err := s.BindSubscription(ctx, 1, p.ID, "old-first")
	if err != nil {
		t.Fatal(err)
	}
	p = subscriptionGroupPlan(t, s, pool, "premium")
	second, err := s.BindSubscription(ctx, 1, p.ID, "old-second")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EndSubscription(ctx, first, 1, false); err != nil {
		t.Fatal(err)
	}
	// Represent a predecessor that ended before the group-chain fix shipped.
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET prev_user_group='vip' WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	if err = s.EndSubscription(ctx, second, 1, false); err != nil {
		t.Fatal(err)
	}
	assertUserGroup(t, pool, "default")
}

func TestSubscriptionGroupsUpgradeUnlinksPriorGroupForOverlappingPackages(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	initial := subscriptionGroupPlan(t, s, pool, "vip")
	packageCallback(t, s, create(t, s, initial.ID))
	first := onlySubscription(t, s)
	p := subscriptionGroupPlan(t, s, pool, "premium")
	second, err := s.BindSubscription(ctx, 1, p.ID, "other-package")
	if err != nil {
		t.Fatal(err)
	}
	target := subscriptionGroupPlan(t, s, pool, "premium")
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.plans SET price_minor=2000,credits=2000 WHERE id=$1`, target.ID); err != nil {
		t.Fatal(err)
	}
	spendPackage(t, pool, first.AccountID, 400, "overlap-upgrade-usage")
	upgrade, err := s.Create(ctx, packageRequest(target.ID, first.ID, "upgrade", "overlap-upgrade"))
	if err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, upgrade)
	if err = s.EndSubscription(ctx, first.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	assertUserGroup(t, pool, "premium")
	if err = s.EndSubscription(ctx, second, 1, false); err != nil {
		t.Fatal(err)
	}
	assertUserGroup(t, pool, "default")
}

func TestSubscriptionGroupsUpgradeToPlainPlanEndsGroupBenefit(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	initial := subscriptionGroupPlan(t, s, pool, "vip")
	packageCallback(t, s, create(t, s, initial.ID))
	sub := onlySubscription(t, s)
	target := packagePlan(t, s, 2000, 2000)
	spendPackage(t, pool, sub.AccountID, 400, "plain-upgrade-usage")
	upgrade, err := s.Create(ctx, packageRequest(target.ID, sub.ID, "upgrade", "plain-upgrade"))
	if err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, upgrade)
	assertUserGroup(t, pool, "default")
	if err = s.EndSubscription(ctx, sub.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	assertUserGroup(t, pool, "default")
}

func assertUserGroup(t *testing.T, pool *pgxpool.Pool, group string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(context.Background(), `SELECT group_name FROM v3_identity.users WHERE id=1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != group {
		t.Fatalf("user group=%q want=%q", got, group)
	}
}
