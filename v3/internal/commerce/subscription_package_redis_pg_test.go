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

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func TestSubscriptionPackageLiveHoldsDeferQuoteAndStaleGatewayCannotSpendFrozenDiscount(t *testing.T) {
	addr := os.Getenv("V3_TEST_COMMERCE_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_COMMERCE_REDIS_ADDR not set")
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr, DB: 11})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	pool := isolatedPool(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := commerce.New(pool, ledger.NewPoster(pool, rdb), []commerce.PaymentProvider{fakePayment{}}, commerce.Config{Now: func() time.Time { return now }, ReturnOrigins: []string{"https://site.test"}, FundingDrain: ledger.NewDrainChecker(rdb)})
	p := packagePlan(t, s, 1000, 1000)
	packageCallback(t, s, create(t, s, p.ID))
	before := onlySubscription(t, s)
	spendPackage(t, pool, before.AccountID, 400, "package-live:prior-usage")
	accounts := ledger.NewAccounts(pool)
	wallet, err := accounts.WalletAccount(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	profile := ledger.NewFundingAccounts(accounts, s, func() time.Time { return now })
	if err = profile.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	prices := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Name: "default", Multiplier: 1}}, Prices: map[string]catalog.Price{"gpt": {Model: "gpt", Mode: "per_token", InputPerMTok: 1_000_000, OutputPerMTok: 2_000_000}}}
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return prices }, profile, accounts, billing.Config{Now: func() time.Time { return now }}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "package-live-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	var accountIDs = []int64{before.AccountID, wallet}
	var requests []*gateway.Request
	cleanup := func() {
		for _, id := range accountIDs {
			_ = rdb.Del(ctx, billing.BalanceKey(id), billing.ReservationIndexKey(id)).Err()
			for _, req := range requests {
				rsv, done, _, member := billing.PostingKeys(id, req.ID)
				_ = rdb.Del(ctx, rsv, done).Err()
				_ = rdb.ZRem(ctx, redisx.KeyReservationOpen, member).Err()
			}
		}
		rows, readErr := pool.Query(ctx, `SELECT id,account_id,operation_id FROM v3_billing.ledger_entries`)
		if readErr == nil {
			for rows.Next() {
				var id, account int64
				var operation string
				if rows.Scan(&id, &account, &operation) != nil {
					break
				}
				rsv, done, _, member := billing.PostingKeys(account, billing.PostingReservationID(operation))
				_ = rdb.Del(ctx, rsv, done, redisx.KeyBalancePrefix+"post:"+strconv.FormatInt(id, 10)).Err()
				_ = rdb.ZRem(ctx, redisx.KeyReservationOpen, member).Err()
				_ = rdb.ZRem(ctx, redisx.KeyPostingOpen, member).Err()
			}
			rows.Close()
		}
		messages, _ := rdb.XRange(ctx, redisx.StreamBillingEvents, "-", "+").Result()
		for _, msg := range messages {
			if value, ok := msg.Values[billing.FieldRequestID].(string); ok && strings.HasPrefix(value, prefix) {
				_ = rdb.XDel(ctx, redisx.StreamBillingEvents, msg.ID).Err()
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	if err = settler.WarmBalances(ctx, accountIDs); err != nil {
		t.Fatal(err)
	}
	relay := ledger.NewBalanceRelay(pool, rdb, quiet)
	if _, err = relay.Step(ctx); err != nil {
		t.Fatal(err)
	}
	request := func(suffix string) *gateway.Request {
		req := &gateway.Request{ID: prefix + suffix, Model: "gpt", Body: []byte(`{"model":"gpt","max_tokens":100}`), Principal: gateway.Principal{UserID: 1, KeyID: 1, Group: "default"}}
		requests = append(requests, req)
		return req
	}
	live := request("-existing-stream")
	if err = settler.Reserve(ctx, live); err != nil {
		t.Fatal(err)
	}
	o, err := s.Create(ctx, packageRequest(p.ID, before.ID, "renew", "package-live-pending"))
	if !errors.Is(err, commerce.ErrFundingPending) || o.ID == 0 {
		t.Fatalf("quoted before held stream drained: order=%+v err=%v", o, err)
	}
	if sources, err := s.ActiveFundingSources(ctx); err != nil || len(sources) != 0 {
		t.Fatalf("preparing quote remains in funding profiles: %+v err=%v", sources, err)
	}
	if err = settler.Reserve(ctx, request("-while-held")); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("stale profile admitted frozen allowance: %v", err)
	}
	if err = settler.Finalize(ctx, live, gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RecoverPackageCheckouts(ctx, 100); err != nil || n != 1 {
		t.Fatalf("durable quote did not resume after drainage: n=%d err=%v", n, err)
	}
	quoted, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || quoted.AmountMinor != 400 || quoted.PaymentURL == "" {
		t.Fatalf("resumed immutable quote=%+v err=%v", quoted, err)
	}
	if err = settler.Reserve(ctx, request("-while-quoted")); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("stale gateway spent quoted discount: %v", err)
	}
	if err = s.Cancel(ctx, 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	after := onlySubscription(t, s)
	accountIDs = append(accountIDs, after.AccountID)
	if after.AccountID == before.AccountID || after.Balance != 600 {
		t.Fatalf("live canceled checkout lost funds: %+v", after)
	}
	if err = profile.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if err = settler.WarmBalances(ctx, []int64{after.AccountID}); err != nil {
		t.Fatal(err)
	}
	if _, err = relay.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if closed, err := rdb.HGet(ctx, billing.BalanceKey(before.AccountID), "closed").Result(); err != nil || closed != "1" {
		t.Fatalf("retired quote account reopened: closed=%q err=%v", closed, err)
	}
	restored := request("-restored")
	if err = settler.Reserve(ctx, restored); err != nil {
		t.Fatalf("gateway cannot spend restored600 credits: %v", err)
	}
	if err = settler.Finalize(ctx, restored, gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput}); err != nil {
		t.Fatal(err)
	}
}
