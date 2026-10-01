//go:build pgintegration

package commerce_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestSubscriptionGroupsExpirationAndNewPaymentsShareUserLockOrder(t *testing.T) {
	s, pool, now := newService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p := subscriptionGroupPlan(t, s, pool, "vip")
	packageCallback(t, s, create(t, s, p.ID))
	old := onlySubscription(t, s)
	*now = old.ExpiresAt.Add(time.Second)
	orders := make([]commerce.Order, 8)
	for i := range orders {
		orders[i] = create(t, s, p.ID)
	}
	start := make(chan struct{})
	results := make(chan error, 9)
	var wg sync.WaitGroup
	for _, order := range orders {
		wg.Add(1)
		go func(o commerce.Order) {
			defer wg.Done()
			<-start
			results <- s.Fulfill(ctx, "test", payment(o))
		}(order)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_, err := s.ExpireSubscriptions(ctx, 100)
		results <- err
	}()
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	// An expiry worker may skip the busy user; its next sweep must converge.
	if _, err := s.ExpireSubscriptions(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var active, expired int
	var group string
	if err := pool.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM v3_commerce.subscriptions WHERE state='active'),
	 (SELECT count(*) FROM v3_commerce.subscriptions WHERE state='expired'),
	 (SELECT group_name FROM v3_identity.users WHERE id=1)`).Scan(&active, &expired, &group); err != nil {
		t.Fatal(err)
	}
	if active != 8 || expired != 1 || group != "vip" {
		t.Fatalf("concurrent lifecycle lost entitlement active=%d expired=%d group=%s", active, expired, group)
	}
}
