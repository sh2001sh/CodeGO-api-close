//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestWalletRewardOverviewAndTransferRespectExactRelease(t *testing.T) {
	s, pool, now := walletFixture(t)
	*now = time.Now().UTC()
	ctx := context.Background()
	walletSetup(t, s, 1)
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET created_at=$1 WHERE id=1`, *now); err != nil {
		t.Fatal(err)
	}
	var account int64
	if err := pool.QueryRow(ctx, `SELECT id FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: account, Amount: 5_000_000, Kind: "reward", Reason: "blind_box_reward", OperationID: "wallet-native-box-reward"}); err != nil {
		t.Fatal(err)
	}
	before, err := s.WalletOverview(ctx, 1, 1, 10)
	if err != nil || before.Balance != 10_000_000 || before.Transferable != 5_000_000 || before.RewardLocked != 5_000_000 {
		t.Fatalf("new reward display=%+v err=%v", before, err)
	}
	in := walletRequest("native-reward-transfer")
	in.Amount = 7_000_000
	if _, err = s.CreateWalletTransfer(ctx, 1, in); !errors.Is(err, commerce.ErrWalletRewardLocked) {
		t.Fatalf("locked money transferred: %v", err)
	}
	if sender, recipient, fees := walletBalances(t, pool); sender != 10_000_000 || recipient != 5_000_000 || fees != 0 {
		t.Fatalf("rejected transfer posted: %d/%d/%d", sender, recipient, fees)
	}
	// Move the immutable test hold's origin to exactly 48 hours before the
	// service clock. Production posts still use their real clock for the guard.
	if _, err = pool.Exec(ctx, `UPDATE v3_billing.wallet_reward_holds SET user_created_at=$1 WHERE account_id=$2`, now.Add(-48*time.Hour), account); err != nil {
		t.Fatal(err)
	}
	after, err := s.WalletOverview(ctx, 1, 1, 10)
	if err != nil || after.Transferable != 7_500_000 || after.RewardLocked != 2_500_000 {
		t.Fatalf("48 hour release display=%+v err=%v", after, err)
	}
	if _, err = s.CreateWalletTransfer(ctx, 1, in); err != nil {
		t.Fatal(err)
	}
	if sender, recipient, fees := walletBalances(t, pool); sender != 2_930_000 || recipient != 12_000_000 || fees != 70_000 {
		t.Fatalf("released transfer balances: %d/%d/%d", sender, recipient, fees)
	}
}

func TestWalletRewardNegativeOverdraftShowsNoTransferableMoney(t *testing.T) {
	s, pool, now := walletFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET balance=$1 WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`, int64(math.MinInt64)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.wallet_reward_holds(account_id,user_id,original_amount,user_created_at,hold_id,source_account_id,idempotency_key)
	 SELECT id,1,1,$1,'negative-balance-reward','negative-balance-reward','negative-balance-reward' FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`, *now); err != nil {
		t.Fatal(err)
	}
	got, err := s.WalletOverview(ctx, 1, 1, 10)
	if err != nil || got.Balance != math.MinInt64 || got.RewardLocked != 1 || got.Transferable != 0 {
		t.Fatalf("overdraft wrapped into spendable money: %+v err=%v", got, err)
	}
}
