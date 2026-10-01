//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
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

func TestSubscriptionCycleDrainsAndKeepsRetiredRedisBucketClosed(t *testing.T) {
	addr := os.Getenv("V3_TEST_COMMERCE_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_COMMERCE_REDIS_ADDR not set")
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr, DB: 12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	pool := isolatedPool(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)
	s := commerce.New(pool, ledger.NewPoster(pool, rdb), []commerce.PaymentProvider{fakePayment{}}, commerce.Config{Now: func() time.Time { return now }, ReturnOrigins: []string{"https://site.test"}, FundingDrain: ledger.NewDrainChecker(rdb)})
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "drained cycle", PriceMinor: 100, Currency: "usd", Credits: 1000, PeriodCredits: 400, PeriodSeconds: 600, ResetPeriod: "custom", ResetCustomSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	old := sub.AccountID
	key := billing.BalanceKey(old)
	operation := "subscription:cycle:close:" + strconv.FormatInt(sub.ID, 10) + ":periodic:" + strconv.FormatInt(now.Add(time.Minute).Unix(), 10)
	reservation := billing.PostingReservationID(operation)
	res, done, holds, member := billing.PostingKeys(old, reservation)
	ownedKeys := []string{key, billing.BalanceKey(old + 1), res, done, holds, redisx.KeyBalancePrefix + "post:1", redisx.KeyBalancePrefix + "post:2", redisx.KeyBalancePrefix + "post:3", redisx.KeyBalancePrefix + "post:4"}
	if err = rdb.Del(ctx, ownedKeys...).Err(); err != nil {
		t.Fatal(err)
	}
	clean := func() {
		_ = rdb.Del(ctx, ownedKeys...).Err()
		_ = rdb.ZRem(ctx, redisx.KeyReservationOpen, member).Err()
		_ = rdb.ZRem(ctx, redisx.KeyPostingOpen, member).Err()
	}
	clean()
	t.Cleanup(clean)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	relay := ledger.NewBalanceRelay(pool, rdb, quiet)
	if _, err = relay.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if err = rdb.HSet(ctx, key, "balance", 400, "reserved", 100, "ver", 1, "base", 1).Err(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if n, err := s.ResetDueSubscriptions(ctx, 100); err != nil || n != 0 {
		t.Fatalf("live hold reset n=%d err=%v", n, err)
	}
	if err = rdb.HSet(ctx, key, "balance", 300, "reserved", 0, "ver", 2, "base", 1).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ResetDueSubscriptions(ctx, 100); err != nil || n != 0 {
		t.Fatalf("unposted usage reset n=%d err=%v", n, err)
	}
	if err = rdb.XGroupCreateMkStream(ctx, redisx.StreamBillingEvents, redisx.GroupLedger, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		t.Fatal(err)
	}
	eventID, err := rdb.XAdd(ctx, &redis.XAddArgs{Stream: redisx.StreamBillingEvents, Values: map[string]any{billing.FieldRequestID: "cycle-real-usage-" + strconv.FormatInt(time.Now().UnixNano(), 10), billing.FieldAccountID: old, billing.FieldAmount: 100}}).Result()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.XDel(ctx, redisx.StreamBillingEvents, eventID).Err() })
	worker, err := ledger.NewWorker(pool, rdb, ledger.WorkerConfig{Consumer: "commerce-cycle", Block: time.Millisecond}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := worker.Step(ctx); err != nil || n != 1 {
		t.Fatalf("real usage posting n=%d err=%v", n, err)
	}
	if n, err := s.ResetDueSubscriptions(ctx, 100); err != nil || n != 1 {
		t.Fatalf("drained reset n=%d err=%v", n, err)
	}
	after := onlySubscription(t, s)
	if after.Balance != 400 || after.UsedCredits != 100 || after.AccountID == old {
		t.Fatalf("drained accounting %+v", after)
	}
	newKey := billing.BalanceKey(after.AccountID)
	if err = rdb.Del(ctx, newKey).Err(); err != nil {
		t.Fatal(err)
	}
	accounts := ledger.NewAccounts(pool)
	settler, err := billing.New(rdb, nil, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err = settler.WarmBalances(ctx, []int64{after.AccountID}); err != nil {
		t.Fatal(err)
	}
	if _, err = relay.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if closed, err := rdb.HGet(ctx, key, "closed").Result(); err != nil || closed != "1" {
		t.Fatalf("retired bucket reopened: %q %v", closed, err)
	}
	if balance, err := rdb.HGet(ctx, key, "balance").Int64(); err != nil || balance != 0 {
		t.Fatalf("retired Redis balance=%d err=%v", balance, err)
	}
	if balance, err := rdb.HGet(ctx, newKey, "balance").Int64(); err != nil || balance != 400 {
		t.Fatalf("new Redis balance=%d err=%v", balance, err)
	}
	if closed, err := rdb.HGet(ctx, newKey, "closed").Result(); err != nil && !errors.Is(err, redis.Nil) {
		t.Fatal(err)
	} else if closed == "1" {
		t.Fatal("fresh cycle bucket closed")
	}
	t.Cleanup(func() {
		_ = rdb.Del(ctx, key, newKey, res, done, holds, redisx.KeyBalancePrefix+"post:1", redisx.KeyBalancePrefix+"post:2", redisx.KeyBalancePrefix+"post:3", redisx.KeyBalancePrefix+"post:4").Err()
		_ = rdb.ZRem(ctx, redisx.KeyReservationOpen, member).Err()
		_ = rdb.ZRem(ctx, redisx.KeyPostingOpen, member).Err()
	})
}
