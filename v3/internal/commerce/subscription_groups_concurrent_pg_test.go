//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestPaymentWaitsForUserBeforeOrderNotification(t *testing.T) {
	s, pool, _ := newService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := subscriptionGroupPlan(t, s, pool, "vip")
	o := create(t, s, p.ID)
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err = blocker.Exec(ctx, `SELECT id FROM v3_identity.users WHERE id=1 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- s.Fulfill(ctx, "test", payment(o)) }()
	// Observe the real blocked statement. The paid-order trigger inserts an
	// inbox row and takes an FK KEY SHARE lock; it must run only after the
	// user's lifecycle lock, otherwise concurrent callbacks deadlock upgrading.
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		var query string
		err = pool.QueryRow(ctx, `SELECT query FROM pg_stat_activity
		 WHERE datname=current_database() AND pid<>pg_backend_pid()
		 AND wait_event_type='Lock' LIMIT 1`).Scan(&query)
		if err == nil {
			if !strings.HasPrefix(query, "SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE") {
				t.Fatalf("payment reached notification before locking user: %s", query)
			}
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		select {
		case err = <-result:
			t.Fatalf("payment did not wait for user: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tick.C:
		}
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	var grants, notices int
	var state string
	if err = pool.QueryRow(ctx, `SELECT state,
	 (SELECT count(*) FROM v3_commerce.subscriptions WHERE order_id=$1),
	 (SELECT count(*) FROM v3_identity.notifications WHERE dedupe_key='order:'||$1::text||':paid')
	 FROM v3_commerce.orders WHERE id=$1`, o.ID).Scan(&state, &grants, &notices); err != nil {
		t.Fatal(err)
	}
	if state != "paid" || grants != 1 || notices != 1 {
		t.Fatalf("payment did not commit benefit and notice together: state=%s grants=%d notices=%d", state, grants, notices)
	}
}

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
