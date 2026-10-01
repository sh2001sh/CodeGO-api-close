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
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func rewardAccount(t *testing.T, pool *pgxpool.Pool, now time.Time) (int64, *Poster) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username,created_at) OVERRIDING SYSTEM VALUE VALUES(7,'reward-user',$1)`, now.Add(-12*time.Hour)); err != nil {
		t.Fatal(err)
	}
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	p.now = func() time.Time { return now }
	return account, p
}

func rewardPost(t *testing.T, p *Poster, account int64, amount credits.Micro, kind, reason, key string) {
	t.Helper()
	if _, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: amount, Kind: kind, Reason: reason, OperationID: key}); err != nil {
		t.Fatal(err)
	}
}

func lockedReward(t *testing.T, pool *pgxpool.Pool, account int64, now time.Time) credits.Micro {
	t.Helper()
	var locked credits.Micro
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		locked, err = LockedRewardAmountTx(ctx, tx, account, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return locked
}

func TestWalletRewardHoldsOwnerSpendFIFOAndTransferCannotBypass(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	account, p := rewardAccount(t, pool, now)
	rewardPost(t, p, account, 100, "topup", "", "paid")
	rewardPost(t, p, account, 100, "reward", "blind_box_reward", "first")
	p.now = func() time.Time { return now.Add(time.Minute) }
	rewardPost(t, p, account, 50, "reward", "blind_box_reward", "second")
	rewardPost(t, p, account, -75, "usage", "", "owner-spend")
	var first, second int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT consumed_amount FROM v3_billing.wallet_reward_holds WHERE reference_id='first'),(SELECT consumed_amount FROM v3_billing.wallet_reward_holds WHERE reference_id='second')`).Scan(&first, &second); err != nil || first != 75 || second != 0 {
		t.Fatalf("hold FIFO=%d/%d %v", first, second, err)
	}
	transfer := billing.Entry{AccountID: account, Amount: -101, Kind: "transfer", Reason: "wallet_peer_transfer_debit", OperationID: "blocked-transfer"}
	if _, err := p.Post(ctx, transfer); !errors.Is(err, ErrWalletRewardTransferLocked) {
		t.Fatalf("transfer bypassed holds: %v", err)
	}
	if bal, _ := pgBalance(t, pool, account); bal != 175 {
		t.Fatalf("failed transfer balance=%d", bal)
	}
	transfer.Amount, transfer.OperationID = -100, "unlocked-transfer"
	for range 2 {
		if _, err := p.Post(ctx, transfer); err != nil {
			t.Fatal(err)
		}
	}
	if got := lockedReward(t, pool, account, now); got != 75 {
		t.Fatalf("peer transfer consumed holds: %d", got)
	}
	rewardPost(t, p, account, -75, "transfer", "blind_box_purchase", "owner-purchase")
	if bal, _ := pgBalance(t, pool, account); bal != 0 || lockedReward(t, pool, account, now) != 0 {
		t.Fatal("owner purchase could not spend held reward")
	}
}

func TestWalletRewardHoldsAgeSnapshotUnknownAndReplay(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	account, p := rewardAccount(t, pool, now)
	rewardPost(t, p, account, 101, "reward", "blind_box_reward", "held")
	for _, tc := range []struct {
		after time.Duration
		want  credits.Micro
	}{{0, 101}, {12 * time.Hour, 101}, {36 * time.Hour, 51}, {60 * time.Hour, 0}} {
		if got := lockedReward(t, pool, account, now.Add(tc.after)); got != tc.want {
			t.Fatalf("after=%s locked=%d want=%d", tc.after, got, tc.want)
		}
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return CreateWalletRewardHoldTx(ctx, tx, account, 7, 102, "held", now) })
	if !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("hold replay conflict=%v", err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return CreateWalletRewardHoldTx(ctx, tx, account, 7, 101, "held", now.Add(100*time.Hour))
	}); err != nil {
		t.Fatal(err)
	}
	if count(t, pool, "wallet_reward_holds") != 1 {
		t.Fatal("hold replay duplicated")
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.wallet_reward_holds SET user_created_at=NULL`); err != nil {
		t.Fatal(err)
	}
	if got := lockedReward(t, pool, account, now); got != 0 {
		t.Fatalf("imported unknown age locked=%d", got)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET created_at=$1 WHERE id=7`, now.Add(-80*time.Hour)); err != nil {
		t.Fatal(err)
	}
	rewardPost(t, p, account, 50, "reward", "blind_box_reward", "old-user-reward")
	rewardPost(t, p, account, 50, "reward", "group_buy_bonus", "group-bonus")
	if count(t, pool, "wallet_reward_holds") != 1 {
		t.Fatal("old-user/nonblindbox reward created hold")
	}
}

func TestWalletRewardConcurrentTransferAndSpendShareAccountLock(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	account, p := rewardAccount(t, pool, now)
	rewardPost(t, p, account, 100, "topup", "", "paid")
	rewardPost(t, p, account, 100, "reward", "blind_box_reward", "held")
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			reason := ""
			kind := "usage"
			if i == 1 {
				reason = "wallet_peer_transfer_debit"
				kind = "transfer"
			}
			if _, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: -100, Kind: kind, Reason: reason, OperationID: fmt.Sprintf("parallel-%d", i)}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if bal, _ := pgBalance(t, pool, account); bal != 0 || lockedReward(t, pool, account, now) != 0 {
		t.Fatal("parallel transfer/spend leaked funds or deadlocked")
	}
}

func TestFundingUsageSplitSkipsKeyBudgetAndConsumesWalletHold(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	wallet, p := rewardAccount(t, pool, now)
	rewardPost(t, p, wallet, 1000, "reward", "blind_box_reward", "held")
	var subscription, key int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES('subscription',1,'subscription',1000) RETURNING id`).Scan(&subscription); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES('api_key',1,'key_budget',1000) RETURNING id`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	primary := usageHookEvent(t, subscription, "mixed-funding", 60, false)
	primary.fields["funding_part"], primary.fields["usage_total_amount"] = "primary", "100"
	primary.fields[billing.FieldBillingSource], primary.fields[billing.FieldMarketGross] = "mixed", "9999"
	primary.fields["procurement_cost_multiplier_ppm"], primary.fields["subscription_id"] = "300000", "1"
	primary.fingerprint = fingerprint(primary.fields)
	secondary := usageHookEvent(t, wallet, "mixed-funding", 40, false)
	secondary.fields["funding_part"] = "secondary"
	secondary.fingerprint = fingerprint(secondary.fields)
	mirror := usageHookEvent(t, key, "mixed-funding", 100, false)
	mirror.fields["funding_part"] = "secondary"
	mirror.fingerprint = fingerprint(mirror.fields)
	if _, err := post(ctx, pool, []event{primary, secondary, mirror}, nil); err != nil {
		t.Fatal(err)
	}
	var amount, actual int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT sum(amount) FROM v3_billing.funding_allocations),actual_amount FROM v3_billing.request_economics WHERE request_id='mixed-funding'`).Scan(&amount, &actual); err != nil || amount != 40 || actual != 100 {
		t.Fatalf("real funding/economics=%d/%d %v", amount, actual, err)
	}
	if got := lockedReward(t, pool, wallet, now); got != 960 {
		t.Fatalf("wallet hold consumed %d, expected locked960", got)
	}
}
