//go:build pgintegration

package ledger

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func revokeConversionV2(pool *pgxpool.Pool, p *Poster, operation string) (revoked, consumed credits.Micro, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		revoked, consumed, err = p.RevokeSubscriptionConversionTx(ctx, tx, 13, 27, operation)
		return err
	})
	return revoked, consumed, err
}

func seedConversionV2(t *testing.T, p *Poster, account int64, paid, reward credits.Micro) {
	t.Helper()
	if paid > 0 {
		fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: paid, Kind: "transfer", OperationID: "provider-original-paid", Metadata: conversionFundingMetadata(int64(paid), 0)})
	}
	if reward > 0 {
		fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: reward, Kind: "transfer", OperationID: "provider-original-reward", Metadata: conversionFundingMetadata(0, int64(reward))})
	}
}

func TestFundingConversionRefundV2ExactOriginUnrelatedPrincipalAndReplay(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	rewardPost(t, p, account, 100, "topup", "", "older-unrelated-paid")
	seedConversionV2(t, p, account, 80, 20)
	other := conversionFundingMetadata(40, 0)
	other["subscription_id"], other["original_order_id"] = int64(14), int64(28)
	fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: 40, Kind: "transfer", OperationID: "unrelated-conversion", Metadata: other})
	for _, operation := range []string{"converted-provider-refund", "converted-provider-refund"} {
		revoked, consumed, err := revokeConversionV2(pool, p, operation)
		if err != nil || revoked != 100 || consumed != 0 {
			t.Fatalf("revocation/replay=%d/%d err=%v", revoked, consumed, err)
		}
	}
	if balance, _ := pgBalance(t, pool, account); balance != 140 {
		t.Fatalf("revocation consumed unrelated principal: %d", balance)
	}
	var topup, unrelated, target int64
	if err := pool.QueryRow(ctx, `SELECT
	 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE source='topup'),
	 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE metadata->>'subscription_id'='14'),
	 (SELECT sum(remaining_amount) FROM v3_billing.funding_lots WHERE metadata->>'subscription_id'='13')`).Scan(&topup, &unrelated, &target); err != nil || topup != 100 || unrelated != 40 || target != 0 {
		t.Fatalf("origin lots=%d/%d/%d err=%v", topup, unrelated, target, err)
	}
	if count(t, pool, "subscription_conversion_revocations") != 1 || count(t, pool, "funding_allocations") != 2 {
		t.Fatal("provider replay duplicated receipt or target allocations")
	}
	if _, _, err := revokeConversionV2(pool, p, "another-provider-delivery"); !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("same origin revoked under another operation: %v", err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, _, err := p.RevokeSubscriptionConversionTx(ctx, tx, 14, 28, "converted-provider-refund")
		return err
	}); !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("revocation operation reused across origins: %v", err)
	}
}

func TestFundingConversionRefundV2ConsumedExposureNeverUsesAnotherGrant(t *testing.T) {
	for _, spent := range []credits.Micro{30, 100} {
		t.Run(fmt.Sprint(int64(spent)), func(t *testing.T) {
			pool := testPool(t)
			account := fundedAccount(t, pool, 7, 0)
			p := NewPoster(pool)
			seedConversionV2(t, p, account, 80, 20)
			rewardPost(t, p, account, -spent, "usage", "", "consumed-conversion")
			rewardPost(t, p, account, 50, "topup", "", "later-unrelated-paid")
			entries := count(t, pool, "ledger_entries")
			for range 2 {
				revoked, consumed, err := revokeConversionV2(pool, p, "consumed-provider-refund")
				if err != nil || revoked != 100-spent || consumed != spent {
					t.Fatalf("exposure=%d/%d err=%v", revoked, consumed, err)
				}
			}
			if balance, _ := pgBalance(t, pool, account); balance != 50 {
				t.Fatalf("consumed origin seized unrelated new payment: %d", balance)
			}
			if spent == 100 && count(t, pool, "ledger_entries") != entries {
				t.Fatal("fully consumed origin created fake zero-amount ledger entry")
			}
		})
	}
}

func TestFundingConversionRefundV2BusinessFailureRollsBackReceiptMoneyAndAudit(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	seedConversionV2(t, p, account, 80, 20)
	entries, outbox := count(t, pool, "ledger_entries"), count(t, pool, "balance_outbox")
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, _, err := p.RevokeSubscriptionConversionTx(ctx, tx, 13, 27, "rollback-provider-refund"); err != nil {
			return err
		}
		return errors.New("commerce refund state failed")
	})
	if err == nil {
		t.Fatal("refund business failure swallowed")
	}
	if balance, _ := pgBalance(t, pool, account); balance != 100 || count(t, pool, "ledger_entries") != entries || count(t, pool, "balance_outbox") != outbox || count(t, pool, "funding_allocations") != 0 || count(t, pool, "subscription_conversion_revocations") != 0 {
		t.Fatal("rollback committed revocation money, receipt or audit")
	}
	if revoked, consumed, err := revokeConversionV2(pool, p, "rollback-provider-refund"); err != nil || revoked != 100 || consumed != 0 {
		t.Fatalf("retry after refund rollback=%d/%d err=%v", revoked, consumed, err)
	}
}

func conversionPeerV2(t *testing.T, pool *pgxpool.Pool, p *Poster, from, to, fromUser, toUser int64, amount credits.Micro, request string) {
	t.Helper()
	metadata := map[string]any{"request_id": request, "sender_user_id": fromUser, "recipient_user_id": toUser, "amount_micro": int64(amount), "fee_micro": int64(0)}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := p.PostTx(ctx, tx, billing.Entry{AccountID: from, Amount: -amount, Kind: "transfer", OperationID: "wallet-transfer:" + request + ":debit", Reason: "wallet_peer_transfer_debit", Metadata: metadata}); err != nil {
			return err
		}
		_, err := p.PostTx(ctx, tx, billing.Entry{AccountID: to, Amount: amount, Kind: "transfer", OperationID: "wallet-transfer:" + request + ":credit", Reason: "wallet_peer_transfer_credit", Metadata: metadata})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFundingConversionRefundV2RevokesTransitivePeerDescendants(t *testing.T) {
	pool := testPool(t)
	first, second, third := fundedAccount(t, pool, 7, 0), fundedAccount(t, pool, 8, 0), fundedAccount(t, pool, 9, 0)
	p := NewPoster(pool)
	seedConversionV2(t, p, first, 100, 0)
	conversionPeerV2(t, pool, p, first, second, 7, 8, 60, "first-hop")
	conversionPeerV2(t, pool, p, second, third, 8, 9, 40, "second-hop")
	for _, account := range []int64{first, second, third} {
		rewardPost(t, p, account, 50, "topup", "", fmt.Sprintf("peer-unrelated:%d", account))
	}
	revoked, consumed, err := revokeConversionV2(pool, p, "transitive-provider-refund")
	if err != nil || revoked != 100 || consumed != 0 {
		t.Fatalf("transitive revocation=%d/%d err=%v", revoked, consumed, err)
	}
	for _, account := range []int64{first, second, third} {
		if balance, _ := pgBalance(t, pool, account); balance != 50 {
			t.Fatalf("wallet%d unrelated funds changed: %d", account, balance)
		}
	}
	if count(t, pool, "funding_allocations") != 5 || count(t, pool, "subscription_conversion_revocations") != 1 {
		t.Fatal("missing transitive transfer/revocation audit")
	}
}

func TestFundingConversionRefundV2ConcurrentReplayAndOwnerSpend(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	seedConversionV2(t, p, account, 100, 0)
	rewardPost(t, p, account, 50, "topup", "", "concurrent-unrelated")
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Go(func() {
			<-start
			revoked, consumed, err := revokeConversionV2(pool, p, "concurrent-provider-refund")
			if err != nil || revoked+consumed != 100 || (consumed != 0 && consumed != 25) {
				t.Errorf("concurrent revocation=%d/%d err=%v", revoked, consumed, err)
			}
		})
	}
	wg.Go(func() {
		<-start
		_, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: -25, Kind: "usage", OperationID: "concurrent-owner-usage"})
		if err != nil {
			t.Error(err)
		}
	})
	close(start)
	wg.Wait()
	revoked, consumed, err := revokeConversionV2(pool, p, "concurrent-provider-refund")
	if err != nil || revoked+consumed != 100 {
		t.Fatalf("concurrent final receipt=%d/%d %v", revoked, consumed, err)
	}
	if balance, _ := pgBalance(t, pool, account); balance != 150-25-int64(revoked) || balance < 0 || count(t, pool, "subscription_conversion_revocations") != 1 {
		t.Fatalf("concurrency failed conservation: balance=%d revoked=%d", balance, revoked)
	}
}

func TestFundingConversionRefundV2UnknownOriginAndInsufficientBackingRequireReview(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	if _, _, err := revokeConversionV2(pool, p, "unknown-origin"); !errors.Is(err, ErrConversionOriginUnknown) {
		t.Fatalf("missing conversion origin accepted: %v", err)
	}
	seedConversionV2(t, p, account, 100, 0)
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET balance=99 WHERE id=$1`, account); err != nil {
		t.Fatal(err)
	}
	if _, _, err := revokeConversionV2(pool, p, "insufficient-origin-backing"); err == nil {
		t.Fatal("inconsistent backing revoked unrelated debt")
	}
	if balance, _ := pgBalance(t, pool, account); balance != 99 || count(t, pool, "funding_allocations") != 0 || count(t, pool, "subscription_conversion_revocations") != 0 {
		t.Fatal("invalid refund backing committed partial revocation")
	}
}

func TestFundingConversionRefundV2GatewayHoldsAndOutboxArePreserved(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 0)
	seed := NewPoster(pool)
	seedConversionV2(t, seed, account, 80, 20)
	rewardPost(t, seed, account, 100, "topup", "", "redis-unrelated")
	if _, err := NewBalanceRelay(pool, rdb, quiet).Step(ctx); err != nil {
		t.Fatal(err)
	}
	balance, version := pgBalance(t, pool, account)
	setHot(t, rdb, account, "balance", balance, "reserved", 150, "ver", version, "base", version)
	p := NewPoster(pool, rdb)
	if _, _, err := revokeConversionV2(pool, p, "redis-provider-refund"); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("revocation bypassed live funding hold: %v", err)
	}
	if balance, _ := pgBalance(t, pool, account); balance != 200 || count(t, pool, "subscription_conversion_revocations") != 0 {
		t.Fatal("held gateway funds caused partial revocation")
	}
	setHot(t, rdb, account, "reserved", 0)
	if revoked, consumed, err := revokeConversionV2(pool, p, "redis-provider-refund"); err != nil || revoked != 100 || consumed != 0 {
		t.Fatalf("drained refund=%d/%d err=%v", revoked, consumed, err)
	}
	if held := postingHeld(t, rdb, account); held != 100 {
		t.Fatalf("provider revocation lost delivery hold: %d", held)
	}
	for range 2 {
		if _, err := NewBalanceRelay(pool, rdb, quiet).Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if hot := hotOf(t, rdb, account); hot.balance != 100 || postingHeld(t, rdb, account) != 0 {
		t.Fatalf("provider revocation outbox failed: %+v", hot)
	}
	if revoked, consumed, err := revokeConversionV2(pool, p, "redis-provider-refund"); err != nil || revoked != 100 || consumed != 0 || postingHeld(t, rdb, account) != 0 {
		t.Fatalf("provider replay redelivered hold=%d/%d err=%v", revoked, consumed, err)
	}
	if got := lockedReward(t, pool, account, time.Now().Add(time.Hour)); got != 0 {
		t.Fatalf("revoked reward restriction remained: %d", got)
	}
}
