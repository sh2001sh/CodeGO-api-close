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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

type refundFundingResolver struct {
	*ledger.Accounts
	account int64
}

func (a refundFundingResolver) SubscriptionAccounts(context.Context, int64) ([]int64, error) {
	return []int64{a.account}, nil
}

func refundRedis(t *testing.T, pool *pgxpool.Pool) *redisx.Client {
	t.Helper()
	addr := os.Getenv("V3_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_REDIS_ADDR not set")
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr, DB: 13})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	// Isolated databases otherwise reuse account ID 1 on a shared test Redis.
	if _, err = pool.Exec(context.Background(), `SELECT setval('v3_billing.accounts_id_seq',$1)`, time.Now().UnixNano()%1_000_000_000+1_000_000_000); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `SELECT setval('v3_billing.balance_outbox_id_seq',$1)`, time.Now().UnixNano()%1_000_000_000+1_000_000_000); err != nil {
		t.Fatal(err)
	}
	return rdb
}

func TestUserRefundLiveHoldAndUnpostedUsageBlockRemoteRefund(t *testing.T) {
	s, _, pool, provider := refundServices(t)
	ctx := context.Background()
	rdb := refundRedis(t, pool)
	o := paidRefundOrder(t, s, 1000, 0)
	account := refundAccount(t, pool)
	key := billing.BalanceKey(account)
	t.Cleanup(func() { _ = rdb.Del(context.Background(), key).Err() })
	refunds := commerce.NewUserRefunds(pool, ledger.NewPoster(pool, rdb), rdb, provider)
	if _, err := ledger.NewBalanceRelay(pool, rdb, slog.New(slog.NewTextHandler(io.Discard, nil))).Step(ctx); err != nil {
		t.Fatal(err)
	}
	for _, pending := range []struct{ held, version int64 }{{1_000_000, 1}, {0, 2}} {
		if err := rdb.HSet(ctx, key, "balance", 9_000_000, "reserved", pending.held, "ver", pending.version, "base", 1).Err(); err != nil {
			t.Fatal(err)
		}
		if _, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: o.TradeNo}); !errors.Is(err, commerce.ErrFundingPending) {
			t.Fatalf("refund admitted while pending=%+v: %v", pending, err)
		}
		if provider.creates != 0 {
			t.Fatal("remote refund preceded settlement drain")
		}
		if closed, _ := rdb.HGet(ctx, key, "closed").Result(); closed != "" {
			t.Fatal("rejected quote stranded wallet closure")
		}
	}
	if err := rdb.XGroupCreateMkStream(ctx, redisx.StreamBillingEvents, redisx.GroupLedger, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		t.Fatal(err)
	}
	id, err := rdb.XAdd(ctx, &redis.XAddArgs{Stream: redisx.StreamBillingEvents, Values: map[string]any{billing.FieldRequestID: "refund-settled-" + strconv.FormatInt(account, 10), billing.FieldAccountID: strconv.FormatInt(account, 10), billing.FieldAmount: "1000000"}}).Result()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.XDel(context.Background(), redisx.StreamBillingEvents, id).Err() })
	worker, err := ledger.NewWorker(pool, rdb, ledger.WorkerConfig{Consumer: "user-refund-test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := worker.Step(ctx); err != nil || n < 1 {
		t.Fatalf("pending usage not posted: %d %v", n, err)
	}
	result, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: o.TradeNo})
	if err != nil || result.Status != "success" || result.AmountMinor != 882 {
		t.Fatalf("drained refund: %+v %v", result, err)
	}
	if held, err := rdb.HGet(ctx, key, "reserved").Int64(); err != nil || held != 9_000_000 {
		t.Fatalf("refund not held against live gateway: %d %v", held, err)
	}
	relay := ledger.NewBalanceRelay(pool, rdb, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err = relay.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if balance, err := rdb.HGet(ctx, key, "balance").Int64(); err != nil || balance != 0 {
		t.Fatalf("refund delivery: %d %v", balance, err)
	}
	if held, _ := rdb.HGet(ctx, key, "reserved").Int64(); held != 0 {
		t.Fatalf("refund hold leaked: %d", held)
	}
}

func TestUserRefundCrashLeaseRecoveryBlocksGatewayThenReopensOwnToken(t *testing.T) {
	s, _, pool, provider := refundServices(t)
	ctx := context.Background()
	rdb := refundRedis(t, pool)
	paidRefundOrder(t, s, 1000, 0)
	account := refundAccount(t, pool)
	key := billing.BalanceKey(account)
	t.Cleanup(func() { _ = rdb.Del(context.Background(), key).Err() })
	refunds := commerce.NewUserRefunds(pool, ledger.NewPoster(pool, rdb), rdb, provider)
	if err := rdb.HSet(ctx, key, "balance", 10_000_000, "reserved", 0, "ver", 1, "base", 1, "closed", 1, "user_refund_owner", "crash-token").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.user_refund_freezes(account_id,token,expires_at) VALUES($1,'crash-token',now()-interval '1 second')`, account); err != nil {
		t.Fatal(err)
	}
	accounts := ledger.NewAccounts(pool)
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}}, Prices: map[string]catalog.Price{"gpt": {Mode: "per_request", PerRequest: 1000}}}
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, accounts, accounts, billing.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	request := &gateway.Request{ID: "closed-refund-wallet", Model: "gpt", Principal: gateway.Principal{UserID: 1, Group: "default"}}
	if err = settler.Reserve(ctx, request); !errors.Is(err, gateway.ErrInsufficientCredits) {
		if err == nil {
			_ = settler.Finalize(ctx, request, gateway.Outcome{})
		}
		t.Fatalf("gateway admitted new spending against closed refund wallet: %v", err)
	}
	var subscriptionAccount int64
	if err = pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('subscription',99,'subscription') RETURNING id`).Scan(&subscriptionAccount); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Del(context.Background(), billing.BalanceKey(subscriptionAccount)).Err() })
	fundingAccounts := refundFundingResolver{Accounts: accounts, account: subscriptionAccount}
	fundingSettler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, fundingAccounts, accounts, billing.Config{OverdraftCap: 2000}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	fundingRequest := &gateway.Request{ID: "closed-refund-wallet-overdraft", Model: "gpt", Principal: gateway.Principal{UserID: 1, Group: "default"}}
	if err = fundingSettler.Reserve(ctx, fundingRequest); !errors.Is(err, gateway.ErrInsufficientCredits) {
		if err == nil {
			_ = fundingSettler.Finalize(ctx, fundingRequest, gateway.Outcome{})
		}
		t.Fatalf("multi-source overdraft bypassed wallet closure: %v", err)
	}
	if err = refunds.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if closed, _ := rdb.HGet(ctx, key, "closed").Result(); closed != "" {
		t.Fatal("crashed wallet never reopened")
	}
	if err = rdb.HSet(ctx, key, "closed", 1).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_commerce.user_refund_freezes(account_id,token,expires_at) VALUES($1,'unused-token',now()-interval '1 second')`, account); err != nil {
		t.Fatal(err)
	}
	if err = refunds.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if closed, _ := rdb.HGet(ctx, key, "closed").Result(); closed != "1" {
		t.Fatal("recovery reopened a different domain closure")
	}
}

func TestUserRefundEqualVersionsCannotHideDifferentOutstandingMovements(t *testing.T) {
	s, _, pool, provider := refundServices(t)
	ctx := context.Background()
	rdb := refundRedis(t, pool)
	first := paidRefundOrder(t, s, 1000, 0)
	second := paidRefundOrder(t, s, 2000, 0)
	account := refundAccount(t, pool)
	key := billing.BalanceKey(account)
	t.Cleanup(func() { _ = rdb.Del(context.Background(), key).Err() })
	if err := rdb.HSet(ctx, key, "balance", 30_000_000, "reserved", 0, "ver", 2, "base", 2).Err(); err != nil {
		t.Fatal(err)
	}
	relay := ledger.NewBalanceRelay(pool, rdb, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := relay.Step(ctx); err != nil {
		t.Fatal(err)
	}
	// This chargeback affects the later lot. A different pending gateway
	// charge affects the older lot. Both happen to advance each side once.
	if err := s.ConfirmRefundTotal(ctx, "epay", second.TradeNo, "later-partial", "cny", 200); err != nil {
		t.Fatal(err)
	}
	if err := rdb.HSet(ctx, key, "balance", 28_000_000, "ver", 3).Err(); err != nil {
		t.Fatal(err)
	}
	refunds := commerce.NewUserRefunds(pool, ledger.NewPoster(pool, rdb), rdb, provider)
	if _, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: first.TradeNo}); !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatalf("equal-count drain admitted wrong origin: %v", err)
	}
	if provider.creates != 0 {
		t.Fatal("remote refund sent before business/usage convergence")
	}
	if _, err := relay.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if err := rdb.XGroupCreateMkStream(ctx, redisx.StreamBillingEvents, redisx.GroupLedger, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		t.Fatal(err)
	}
	id, err := rdb.XAdd(ctx, &redis.XAddArgs{Stream: redisx.StreamBillingEvents, Values: map[string]any{billing.FieldRequestID: "refund-coincidence-" + strconv.FormatInt(account, 10), billing.FieldAccountID: strconv.FormatInt(account, 10), billing.FieldAmount: "2000000"}}).Result()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.XDel(context.Background(), redisx.StreamBillingEvents, id).Err() })
	worker, err := ledger.NewWorker(pool, rdb, ledger.WorkerConfig{Consumer: "user-refund-test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := worker.Step(ctx); err != nil || n < 1 {
		t.Fatalf("converging usage: %d %v", n, err)
	}
	r, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: first.TradeNo})
	if err != nil || r.Status != "success" || r.AmountMinor != 784 {
		t.Fatalf("converged origin: %+v %v", r, err)
	}
	if _, err = relay.Step(ctx); err != nil {
		t.Fatal(err)
	}
}
