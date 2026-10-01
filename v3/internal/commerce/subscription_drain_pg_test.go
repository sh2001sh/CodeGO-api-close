//go:build pgintegration

package commerce_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// Regression (v3 parity review): an expiry must not debit a stale PG balance
// while an in-flight request still reserves or settles that subscription.
func TestSubscriptionExpiryWaitsForReservedAndUnpostedUsage(t *testing.T) {
	addr := os.Getenv("V3_TEST_COMMERCE_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_COMMERCE_REDIS_ADDR not set")
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr, DB: 14})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	pool := isolatedPool(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)
	poster := ledger.NewPoster(pool, rdb)
	s := commerce.New(pool, poster, []commerce.PaymentProvider{fakePayment{}}, commerce.Config{
		Now: func() time.Time { return now }, ReturnOrigins: []string{"https://site.test"}, FundingDrain: ledger.NewDrainChecker(rdb)})
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "minute", PriceMinor: 500, Currency: "usd", Credits: 1000, PeriodSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 1 {
		t.Fatalf("subs=%+v err=%v", subs, err)
	}
	account := subs[0].AccountID
	key := billing.BalanceKey(account)
	reservationID := billing.PostingReservationID("subscription:end:" + strconv.FormatInt(subs[0].ID, 10))
	reservation, done, holds, member := billing.PostingKeys(account, reservationID)
	ownedKeys := []string{key, reservation, done, holds, redisx.KeyBalancePrefix + "post:1", redisx.KeyBalancePrefix + "post:2"}
	clean := func() {
		t.Helper()
		if err := rdb.Del(ctx, ownedKeys...).Err(); err != nil {
			t.Error(err)
		}
		for _, index := range []string{redisx.KeyReservationOpen, redisx.KeyPostingOpen} {
			if err := rdb.ZRem(ctx, index, member).Err(); err != nil {
				t.Error(err)
			}
		}
	}
	clean()
	t.Cleanup(clean)
	if err = rdb.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	relay := ledger.NewBalanceRelay(pool, rdb, quiet)
	if _, err = relay.Step(ctx); err != nil {
		t.Fatal(err)
	}
	// Hot state includes 100 credits consumed and another request's 100 hold;
	// PostgreSQL still contains the full 1000-credit original grant.
	if err = rdb.HSet(ctx, key, "balance", 900, "reserved", 100, "ver", 2, "base", 1).Err(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	assertPending := func() {
		t.Helper()
		if n, err := s.ExpireSubscriptions(ctx, 100); err != nil || n != 0 {
			t.Fatalf("expired pending bucket: count=%d err=%v", n, err)
		}
		var balance int64
		var state string
		if err := pool.QueryRow(ctx, `SELECT a.balance,s.state FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id WHERE s.id=$1`, subs[0].ID).Scan(&balance, &state); err != nil {
			t.Fatal(err)
		}
		if balance != 1000 || state != "active" {
			t.Fatalf("pending expiry changed PG funds/state: %d %s", balance, state)
		}
	}
	assertPending()
	if closed, err := rdb.HGet(ctx, key, "closed").Result(); err != nil || closed != "1" {
		t.Fatalf("expired subscription still admits new funding: %q %v", closed, err)
	}
	if err = rdb.HSet(ctx, key, "reserved", 0).Err(); err != nil {
		t.Fatal(err)
	}
	assertPending()
	// Consume the same real Redis usage event through the real ledger worker.
	if err = rdb.XGroupCreateMkStream(ctx, redisx.StreamBillingEvents, redisx.GroupLedger, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		t.Fatal(err)
	}
	request := "commerce-expiry-" + strconv.FormatInt(now.UnixNano(), 10)
	id, err := rdb.XAdd(ctx, &redis.XAddArgs{Stream: redisx.StreamBillingEvents, Values: map[string]any{
		billing.FieldRequestID: request, billing.FieldAccountID: account, billing.FieldAmount: 100,
	}}).Result()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = rdb.XDel(ctx, redisx.StreamBillingEvents, id).Err()
	})
	worker, err := ledger.NewWorker(pool, rdb, ledger.WorkerConfig{Consumer: "commerce-expiry", Block: time.Millisecond}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := worker.Step(ctx); err != nil || n != 1 {
		t.Fatalf("usage posting count=%d err=%v", n, err)
	}
	if n, err := s.ExpireSubscriptions(ctx, 100); err != nil || n != 1 {
		t.Fatalf("drained expiry count=%d err=%v", n, err)
	}
	if _, err = relay.Step(ctx); err != nil {
		t.Fatal(err)
	}
	var balance, expiredAmount int64
	if err = pool.QueryRow(ctx, `SELECT a.balance,(SELECT -amount FROM v3_billing.ledger_entries WHERE account_id=a.id AND kind='subscription_expire') FROM v3_billing.accounts a WHERE id=$1`, account).Scan(&balance, &expiredAmount); err != nil {
		t.Fatal(err)
	}
	if balance != 0 || expiredAmount != 900 {
		t.Fatalf("expiry double-spent pending charge: balance=%d expired=%d", balance, expiredAmount)
	}
	if hot, err := rdb.HGet(ctx, key, "balance").Int64(); err != nil || hot != 0 {
		t.Fatalf("Redis expiry balance=%d err=%v", hot, err)
	}
}
