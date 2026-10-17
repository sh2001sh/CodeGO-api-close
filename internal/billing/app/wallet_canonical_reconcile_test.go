package app

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedCanonicalWalletReservation(t *testing.T, userID int) (*billingschema.BillingAccount, *billingschema.BillingReservation) {
	t.Helper()
	truncate(t)
	seedUser(t, userID, 1000)
	account, err := ensureMirroredUserAccount(userID, billingAccountTypeClaudeWallet, 1000)
	require.NoError(t, err)
	reservation, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{
		AccountID: account.AccountID, RequestID: t.Name(), ReservedAmount: 300, IdempotencyKey: t.Name() + ":reserve",
	})
	require.NoError(t, err)
	return account, reservation
}

func assertCanonicalWalletState(t *testing.T, accountID string, available, reserved, granted, consumed int64) {
	t.Helper()
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, platformdb.DB.Where("account_id = ?", accountID).First(&snapshot).Error)
	require.Equal(t, available, snapshot.AvailableBalance)
	require.Equal(t, reserved, snapshot.ReservedBalance)
	require.Equal(t, granted, snapshot.GrantedTotal)
	require.Equal(t, consumed, snapshot.ConsumedTotal)
	require.Equal(t, granted, snapshot.AvailableBalance+snapshot.ReservedBalance+snapshot.ConsumedTotal)
	var reconciliations int64
	require.NoError(t, platformdb.DB.Model(&billingschema.BillingLedgerEntry{}).
		Where("account_id = ? AND reason_code = ?", accountID, "ledger_sync_reconcile").Count(&reconciliations).Error)
	require.Zero(t, reconciliations, "a stale projection cannot create an adjustment")
}

func TestCanonicalWalletCreditPreservesInflightReservation(t *testing.T) {
	for index, complete := range []string{"release", "settle"} {
		t.Run(complete, func(t *testing.T) {
			userID := 89100 + index
			account, reservation := seedCanonicalWalletReservation(t, userID)
			credit := func() error {
				return platformdb.DB.Transaction(func(tx *gorm.DB) error {
					return CreditClaudeWalletQuotaTx(tx, userID, 10, t.Name()+":credit", "daily_checkin_credit")
				})
			}
			require.NoError(t, credit())
			require.NoError(t, credit())
			assertCanonicalWalletState(t, account.AccountID, 710, 300, 1010, 0)
			if complete == "release" {
				_, err := billingdomain.ReleaseReservation(billingdomain.ReleaseReservationParams{
					ReservationID: reservation.ReservationID, IdempotencyKey: t.Name() + ":release",
				})
				require.NoError(t, err)
				assertCanonicalWalletState(t, account.AccountID, 1010, 0, 1010, 0)
			} else {
				_, err := billingdomain.SettleReservation(billingdomain.SettleReservationParams{
					ReservationID: reservation.ReservationID, ActualAmount: 120, IdempotencyKey: t.Name() + ":settle",
				})
				require.NoError(t, err)
				assertCanonicalWalletState(t, account.AccountID, 890, 0, 1010, 120)
			}
		})
	}
}

func TestCanonicalWalletIgnoresBothDirectionsOfStaleProjection(t *testing.T) {
	for index, projection := range []int{100, 2000} {
		t.Run(fmt.Sprint(projection), func(t *testing.T) {
			userID := 89200 + index
			account, _ := seedCanonicalWalletReservation(t, userID)
			require.NoError(t, platformdb.DB.Model(&identityschema.User{}).Where("id = ?", userID).Update("claude_quota", projection).Error)
			require.NoError(t, reconcileAccountBalance(account, userID, projection))
			require.NoError(t, platformdb.DB.Transaction(func(tx *gorm.DB) error {
				return CreditClaudeWalletQuotaTx(tx, userID, 10, t.Name()+":credit", "redemption_credit")
			}))
			assertCanonicalWalletState(t, account.AccountID, 710, 300, 1010, 0)
		})
	}
}

func TestCanonicalWalletCreditCannotRestoreSettledOrExhaustedBalance(t *testing.T) {
	for index, spent := range []int64{300, 1000} {
		t.Run(fmt.Sprint(spent), func(t *testing.T) {
			userID := 89600 + index
			account, reservation := seedCanonicalWalletReservation(t, userID)
			_, err := billingdomain.SettleReservation(billingdomain.SettleReservationParams{
				ReservationID: reservation.ReservationID, ActualAmount: spent, IdempotencyKey: t.Name() + ":settle",
			})
			require.NoError(t, err)
			require.NoError(t, platformdb.DB.Transaction(func(tx *gorm.DB) error {
				return CreditClaudeWalletQuotaTx(tx, userID, 10, t.Name()+":credit", "redemption_credit")
			}))
			assertCanonicalWalletState(t, account.AccountID, 1010-spent, 0, 1010, spent)
		})
	}
}

func TestCanonicalWalletDebitCannotSpendInflightFunds(t *testing.T) {
	account, reservation := seedCanonicalWalletReservation(t, 89300)
	debit := func(amount int) error {
		return platformdb.DB.Transaction(func(tx *gorm.DB) error {
			return DebitClaudeWalletQuotaTxWithReason(tx, 89300, amount, fmt.Sprintf("%s:%d", t.Name(), amount), "wallet_peer_transfer_debit")
		})
	}
	require.ErrorIs(t, debit(800), billingdomain.ErrInsufficientBalance)
	assertCanonicalWalletState(t, account.AccountID, 700, 300, 1000, 0)
	require.NoError(t, debit(100))
	assertCanonicalWalletState(t, account.AccountID, 600, 300, 1000, 100)
	_, err := billingdomain.ReleaseReservation(billingdomain.ReleaseReservationParams{
		ReservationID: reservation.ReservationID, IdempotencyKey: t.Name() + ":release",
	})
	require.NoError(t, err)
	assertCanonicalWalletState(t, account.AccountID, 900, 0, 1000, 100)
}

func TestCanonicalWalletDebitIgnoresLowLegacyProjection(t *testing.T) {
	account, reservation := seedCanonicalWalletReservation(t, 89700)
	require.NoError(t, platformdb.DB.Model(&identityschema.User{}).Where("id = ?", 89700).Update("claude_quota", 100).Error)
	require.NoError(t, platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return DebitClaudeWalletQuotaTxWithReason(tx, 89700, 500, t.Name()+":debit", "wallet_peer_transfer_debit")
	}))
	assertCanonicalWalletState(t, account.AccountID, 200, 300, 1000, 500)
	var user identityschema.User
	require.NoError(t, platformdb.DB.First(&user, 89700).Error)
	require.Zero(t, user.ClaudeQuota, "a stale projection must not authorize or recreate reserved funds")
	require.NoError(t, platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return DebitClaudeWalletQuotaTxWithReason(tx, 89700, 500, t.Name()+":debit", "wallet_peer_transfer_debit")
	}))
	assertCanonicalWalletState(t, account.AccountID, 200, 300, 1000, 500)
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return DebitClaudeWalletQuotaTxWithReason(tx, 89700, 201, t.Name()+":overspend", "wallet_peer_transfer_debit")
	})
	require.ErrorIs(t, err, billingdomain.ErrInsufficientBalance)
	_, err = billingdomain.ReleaseReservation(billingdomain.ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: t.Name() + ":release"})
	require.NoError(t, err)
	assertCanonicalWalletState(t, account.AccountID, 500, 0, 1000, 500)
}

func TestCanonicalWalletDebitReplayPreservesLegacyProjection(t *testing.T) {
	account, _ := seedCanonicalWalletReservation(t, 89800)
	debit := func(amount int) error {
		return platformdb.DB.Transaction(func(tx *gorm.DB) error {
			return DebitClaudeWalletQuotaTxWithReason(tx, 89800, amount, t.Name()+":debit", "wallet_peer_transfer_debit")
		})
	}
	require.NoError(t, debit(100))
	require.NoError(t, debit(100))
	assertCanonicalWalletState(t, account.AccountID, 600, 300, 1000, 100)
	var user identityschema.User
	require.NoError(t, platformdb.DB.First(&user, 89800).Error)
	require.Equal(t, 900, user.ClaudeQuota, "replay must not debit the legacy projection twice")
	require.ErrorIs(t, debit(101), billingdomain.ErrLedgerConflict)
	assertCanonicalWalletState(t, account.AccountID, 600, 300, 1000, 100)
}

func TestCanonicalWalletDebitProjectionFailureRollsBackReceipt(t *testing.T) {
	account, _ := seedCanonicalWalletReservation(t, 89900)
	name := "test:canonical_wallet_debit_projection_failure"
	require.NoError(t, platformdb.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("synthetic debit projection failure"))
		}
	}))
	t.Cleanup(func() { _ = platformdb.DB.Callback().Update().Remove(name) })
	debit := func() error {
		return platformdb.DB.Transaction(func(tx *gorm.DB) error {
			return DebitClaudeWalletQuotaTxWithReason(tx, 89900, 100, t.Name()+":debit", "wallet_peer_transfer_debit")
		})
	}
	require.ErrorContains(t, debit(), "synthetic debit projection failure")
	assertCanonicalWalletState(t, account.AccountID, 700, 300, 1000, 0)
	var receipts int64
	require.NoError(t, platformdb.DB.Model(&billingschema.BillingSettlement{}).Where("idempotency_key = ?", "ledger-settlement:"+t.Name()+":debit").Count(&receipts).Error)
	require.Zero(t, receipts)
	require.NoError(t, platformdb.DB.Callback().Update().Remove(name))
	require.NoError(t, debit())
	require.NoError(t, debit())
	assertCanonicalWalletState(t, account.AccountID, 600, 300, 1000, 100)
	var user identityschema.User
	require.NoError(t, platformdb.DB.First(&user, 89900).Error)
	require.Equal(t, 900, user.ClaudeQuota)
}

func TestCanonicalWalletCreditFailureRollsBackLedger(t *testing.T) {
	account, _ := seedCanonicalWalletReservation(t, 89400)
	name := "test:canonical_wallet_projection_failure"
	require.NoError(t, platformdb.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("synthetic projection write failure"))
		}
	}))
	t.Cleanup(func() { _ = platformdb.DB.Callback().Update().Remove(name) })
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return CreditClaudeWalletQuotaTx(tx, 89400, 10, t.Name()+":credit", "topup_credit")
	})
	require.ErrorContains(t, err, "synthetic projection write failure")
	assertCanonicalWalletState(t, account.AccountID, 700, 300, 1000, 0)
	var credits int64
	require.NoError(t, platformdb.DB.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", t.Name()+":credit").Count(&credits).Error)
	require.Zero(t, credits)
}

func TestCanonicalWalletConcurrentCreditsAndReplayDuringReservation(t *testing.T) {
	account, _ := seedCanonicalWalletReservation(t, 89500)
	const workers = 12
	errors := make(chan error, workers)
	var done sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < workers; worker++ {
		done.Add(1)
		go func(worker int) {
			defer done.Done()
			<-start
			// Six distinct receipts, each submitted twice.
			key := fmt.Sprintf("%s:credit:%d", t.Name(), worker/2)
			errors <- platformdb.DB.Transaction(func(tx *gorm.DB) error {
				return CreditClaudeWalletQuotaTx(tx, 89500, 10, key, "blind_box_reward")
			})
		}(worker)
	}
	close(start)
	done.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	assertCanonicalWalletState(t, account.AccountID, 760, 300, 1060, 0)
	var credits int64
	require.NoError(t, platformdb.DB.Model(&billingschema.BillingLedgerEntry{}).
		Where("account_id = ? AND reason_code = ?", account.AccountID, "blind_box_reward").Count(&credits).Error)
	require.EqualValues(t, workers/2, credits)
}
