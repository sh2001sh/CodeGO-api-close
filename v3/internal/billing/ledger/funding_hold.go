package ledger

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

var ErrWalletRewardTransferLocked = errors.New("ledger: blind-box reward is not transferable yet")

// CreateWalletRewardHoldTx holds current blind-box wallet money for new users.
// The credited money stays spendable. Calling this never posts another credit.
func CreateWalletRewardHoldTx(ctx context.Context, tx pgx.Tx, account, user int64, amount credits.Micro, key string, now time.Time) error {
	if account <= 0 || user <= 0 || amount <= 0 || key == "" {
		return errors.New("ledger: invalid wallet reward hold")
	}
	var owner int64
	if err := tx.QueryRow(ctx, `SELECT owner_id FROM v3_billing.accounts WHERE id=$1 AND owner_type='user' AND kind='wallet' FOR UPDATE`, account).Scan(&owner); err != nil {
		return err
	}
	if owner != user {
		return errors.New("ledger: reward hold owner mismatch")
	}
	var identical bool
	err := tx.QueryRow(ctx, `SELECT account_id=$2 AND user_id=$3 AND original_amount=$4 FROM v3_billing.wallet_reward_holds WHERE idempotency_key=$1`, "native:hold:"+key, account, user, int64(amount)).Scan(&identical)
	if err == nil {
		if !identical {
			return billing.ErrPostConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var created *time.Time
	if err := tx.QueryRow(ctx, `SELECT created_at FROM v3_identity.users WHERE id=$1`, user).Scan(&created); err != nil {
		return err
	}
	if created == nil || created.Unix() <= 0 || now.Sub(*created) < 0 || now.Sub(*created) >= 72*time.Hour {
		return nil // v2 unknown age is released, and only new users create holds
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_billing.wallet_reward_holds
	 (hold_id,source_account_id,account_id,user_id,original_amount,reference_type,reference_id,idempotency_key,user_created_at,created_at)
	 VALUES($1,$2,$3,$4,$5,'blind_box_reward',$6,$7,$8,$9)`, fundingID("hold", key), fmt.Sprintf("native:account:%d", account), account, user, int64(amount), key, "native:hold:"+key, created, now)
	return err
}

// Exact floor release avoids float rounding for balances above 2^53.
func unreleasedReward(original, consumed int64, created *time.Time, now time.Time) int64 {
	if created == nil || created.Unix() <= 0 || now.Sub(*created) >= 72*time.Hour {
		return 0
	}
	age := now.Sub(*created)
	if age <= 24*time.Hour {
		return original - consumed
	}
	released := new(big.Int).Mul(big.NewInt(original), big.NewInt(int64(age-24*time.Hour)))
	released.Quo(released, big.NewInt(int64(48*time.Hour)))
	return max(original-consumed-released.Int64(), 0)
}

// LockedRewardAmountTx locks the account before its holds, sharing the same
// transaction as transfers/spending. NULL imported age is fully released.
func LockedRewardAmountTx(ctx context.Context, tx pgx.Tx, account int64, now time.Time) (credits.Micro, error) {
	_, locked, err := walletRewardStateTx(ctx, tx, account, now)
	return locked, err
}

func TransferableBalanceTx(ctx context.Context, tx pgx.Tx, account int64, now time.Time) (credits.Micro, error) {
	balance, locked, err := walletRewardStateTx(ctx, tx, account, now)
	if err != nil || balance <= locked {
		return 0, err
	}
	return balance - locked, nil
}

func walletRewardStateTx(ctx context.Context, tx pgx.Tx, account int64, now time.Time) (credits.Micro, credits.Micro, error) {
	var balance credits.Micro
	if err := tx.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1 AND kind='wallet' FOR UPDATE`, account).Scan(&balance); err != nil {
		return 0, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT original_amount,consumed_amount,user_created_at FROM v3_billing.wallet_reward_holds
	 WHERE account_id=$1 AND consumed_amount<original_amount ORDER BY created_at,hold_id FOR UPDATE`, account)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var locked credits.Micro
	for rows.Next() {
		var original, consumed int64
		var created *time.Time
		if err := rows.Scan(&original, &consumed, &created); err != nil {
			return 0, 0, err
		}
		locked, err = locked.Add(credits.Micro(unreleasedReward(original, consumed, created, now)))
		if err != nil {
			return 0, 0, err
		}
	}
	return balance, locked, rows.Err()
}

// Private because the caller already owns the account row lock. Peer transfers
// never consume holds; owner spend deliberately consumes these rewards first.
func consumeWalletRewardHoldsTx(ctx context.Context, tx pgx.Tx, account int64, amount credits.Micro) error {
	rows, err := tx.Query(ctx, `SELECT hold_id,original_amount-consumed_amount FROM v3_billing.wallet_reward_holds
	 WHERE account_id=$1 AND consumed_amount<original_amount ORDER BY created_at,hold_id FOR UPDATE`, account)
	if err != nil {
		return err
	}
	type heldReward struct {
		id        string
		available int64
	}
	var holds []heldReward
	for rows.Next() {
		var hold heldReward
		if err := rows.Scan(&hold.id, &hold.available); err != nil {
			rows.Close()
			return err
		}
		holds = append(holds, hold)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	remaining := int64(amount)
	for _, hold := range holds {
		used := min(hold.available, remaining)
		if used == 0 {
			break
		}
		if _, err := tx.Exec(ctx, `UPDATE v3_billing.wallet_reward_holds SET consumed_amount=consumed_amount+$2 WHERE hold_id=$1`, hold.id, used); err != nil {
			return err
		}
		remaining -= used
	}
	return nil
}
