//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func subscriptionGroupPlan(t *testing.T, s *commerce.Service, pool *pgxpool.Pool, group string) commerce.Plan {
	t.Helper()
	p := packagePlan(t, s, 1000, 1000)
	if _, err := pool.Exec(context.Background(), `INSERT INTO v3_catalog.groups(name) VALUES('default'),('vip'),('premium'),('manual') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE v3_commerce.plans SET upgrade_group=$2 WHERE id=$1`, p.ID, group); err != nil {
		t.Fatal(err)
	}
	return p
}

func assertSubscriptionGroup(t *testing.T, pool *pgxpool.Pool, sub int64, group, target, previous string) {
	t.Helper()
	var gotGroup, gotTarget, gotPrevious string
	if err := pool.QueryRow(context.Background(), `SELECT u.group_name,s.upgrade_group,s.prev_user_group
	 FROM v3_commerce.subscriptions s JOIN v3_identity.users u ON u.id=s.user_id WHERE s.id=$1`, sub).
		Scan(&gotGroup, &gotTarget, &gotPrevious); err != nil {
		t.Fatal(err)
	}
	if gotGroup != group || gotTarget != target || gotPrevious != previous {
		t.Fatalf("subscription %d group=%q target=%q previous=%q want %q/%q/%q", sub, gotGroup, gotTarget, gotPrevious, group, target, previous)
	}
}

func TestSubscriptionGroupsGrantConcurrentReplayAndCancellation(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := subscriptionGroupPlan(t, s, pool, "vip")
	o := create(t, s, p.ID)
	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity='user' AND entity_id='1'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.Fulfill(ctx, "test", payment(o))
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	sub := onlySubscription(t, s)
	assertSubscriptionGroup(t, pool, sub.ID, "vip", "vip", "default")
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity='user' AND entity_id='1'`).Scan(&after); err != nil || after <= before {
		t.Fatalf("authoritative group invalidation before=%d after=%d err=%v", before, after, err)
	}
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	var replayed int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity='user' AND entity_id='1'`).Scan(&replayed); err != nil || replayed != after {
		t.Fatalf("replay changed authorization invalidations before=%d after=%d err=%v", after, replayed, err)
	}
	if err := s.EndSubscription(ctx, sub.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "default", "vip", "default")
	if err := s.EndSubscription(ctx, sub.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "default", "vip", "default")
}

func TestSubscriptionGroupsOtherLiveEntitlementAndAdminOverride(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "live entitlement", true: "admin override"}[override], func(t *testing.T) {
			s, pool, _ := newService(t)
			ctx := context.Background()
			p := subscriptionGroupPlan(t, s, pool, "vip")
			packageCallback(t, s, create(t, s, p.ID))
			first := onlySubscription(t, s)
			if override {
				if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET group_name='manual' WHERE id=1`); err != nil {
					t.Fatal(err)
				}
				if err := s.DeleteSubscription(ctx, first.ID); err != nil {
					t.Fatal(err)
				}
				assertSubscriptionGroup(t, pool, first.ID, "manual", "vip", "default")
				return
			}
			packageCallback(t, s, create(t, s, p.ID))
			subs, err := s.ListSubscriptions(ctx, 1)
			if err != nil || len(subs) != 2 {
				t.Fatalf("subscriptions=%+v err=%v", subs, err)
			}
			second := subs[0]
			if err = s.EndSubscription(ctx, second.ID, 1, false); err != nil {
				t.Fatal(err)
			}
			assertSubscriptionGroup(t, pool, first.ID, "vip", "vip", "default")
			if err = s.EndSubscription(ctx, first.ID, 1, false); err != nil {
				t.Fatal(err)
			}
			assertSubscriptionGroup(t, pool, first.ID, "default", "vip", "default")
		})
	}
}

func TestSubscriptionGroupsImportedPolicyExpiresAndIgnoresPlanEdit(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p := subscriptionGroupPlan(t, s, pool, "vip")
	packageCallback(t, s, create(t, s, p.ID))
	sub := onlySubscription(t, s)
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET upgrade_group='vip',prev_user_group='default' WHERE id=$1`, sub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.plans SET upgrade_group='premium' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	*now = sub.ExpiresAt.Add(time.Second)
	if n, err := s.ExpireSubscriptions(ctx, 100); err != nil || n != 1 {
		t.Fatalf("expiration n=%d err=%v", n, err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "default", "vip", "default")
}

func TestSubscriptionGroupsMissingCatalogGroupRollsBackPayment(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := subscriptionGroupPlan(t, s, pool, "missing-group")
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("missing authorization group granted: %v", err)
	}
	var state, group string
	var subs, entries, events int
	if err := pool.QueryRow(ctx, `SELECT state,(SELECT group_name FROM v3_identity.users WHERE id=1),
	 (SELECT count(*) FROM v3_commerce.subscriptions),(SELECT count(*) FROM v3_billing.ledger_entries),
	 (SELECT count(*) FROM v3_commerce.payment_events) FROM v3_commerce.orders WHERE id=$1`, o.ID).
		Scan(&state, &group, &subs, &entries, &events); err != nil {
		t.Fatal(err)
	}
	if state != "created" || group != "default" || subs != 0 || entries != 0 || events != 0 {
		t.Fatalf("partial grant state=%s group=%s subscriptions=%d entries=%d events=%d", state, group, subs, entries, events)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES('missing-group')`); err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, o)
	assertSubscriptionGroup(t, pool, onlySubscription(t, s).ID, "missing-group", "missing-group", "default")
}
