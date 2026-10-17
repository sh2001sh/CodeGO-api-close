package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func TestCanonicalWalletReservationCreditReplayPostgres(t *testing.T) {
	dsn := os.Getenv("WALLET_CANONICAL_TEST_DSN")
	if dsn == "" {
		t.Skip("WALLET_CANONICAL_TEST_DSN requires a dedicated codego_wallet_test_ database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), TranslateError: true})
	require.NoError(t, err)
	var database string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&database).Error)
	require.True(t, strings.HasPrefix(database, "codego_wallet_test_"), "dedicated wallet test database required")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(24)
	previousDB, previousPG, previousSQLite, previousMySQL := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL = db, true, false, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL = previousDB, previousPG, previousSQLite, previousMySQL
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}, &billingschema.FundingLot{}))

	for _, finish := range []string{"release", "settle"} {
		t.Run(finish, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			identity := fmt.Sprintf("walletpg%d", time.Now().UnixNano())
			user := identityschema.User{Username: identity, AffCode: identity, ClaudeQuota: 1000, Status: constant.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			account, err := ensureMirroredUserAccount(user.Id, billingAccountTypeClaudeWallet, 1000)
			require.NoError(t, err)
			reservation, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, RequestID: account.AccountID, ReservedAmount: 300, IdempotencyKey: account.AccountID + ":reserve"})
			require.NoError(t, err)
			credit := func(key string) error {
				return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					return CreditClaudeWalletQuotaTx(tx, user.Id, 10, key, "redemption_credit")
				})
			}
			rollback := errors.New("synthetic post-credit failure")
			err = db.Transaction(func(tx *gorm.DB) error {
				if err := CreditClaudeWalletQuotaTx(tx, user.Id, 10, account.AccountID+":rollback", "redemption_credit"); err != nil {
					return err
				}
				return rollback
			})
			require.ErrorIs(t, err, rollback)
			assertCanonicalWalletState(t, account.AccountID, 700, 300, 1000, 0)

			blocker := db.WithContext(ctx).Begin()
			require.NoError(t, blocker.Error)
			t.Cleanup(func() { _ = blocker.Rollback().Error })
			var locked billingschema.BillingBalanceSnapshot
			require.NoError(t, blocker.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account.AccountID).First(&locked).Error)
			var blockerPID int
			require.NoError(t, blocker.Raw("SELECT pg_backend_pid()").Scan(&blockerPID).Error)
			const workers = 12
			start, results := make(chan struct{}), make(chan error, workers)
			for worker := 0; worker < workers; worker++ {
				key := fmt.Sprintf("%s:receipt:%d", account.AccountID, worker/2)
				go func() {
					<-start
					results <- credit(key)
				}()
			}
			close(start)
			// Verify real, separate PostgreSQL backends compete for the held row.
			deadline := time.Now().Add(5 * time.Second)
			for {
				var waiting int64
				require.NoError(t, db.WithContext(ctx).Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid))", blockerPID).Scan(&waiting).Error)
				if waiting > 0 {
					t.Logf("verified PostgreSQL lock contention: %d independent waiters", waiting)
					break
				}
				require.True(t, time.Now().Before(deadline), "no independent credit transaction waited for the snapshot lock")
				time.Sleep(10 * time.Millisecond)
			}
			require.NoError(t, blocker.Commit().Error)
			for worker := 0; worker < workers; worker++ {
				err := <-results
				if !errors.Is(err, gorm.ErrDuplicatedKey) {
					require.NoError(t, err)
				}
			}
			// A duplicate can lose the unique-key race and roll back; its retry
			// must succeed without either minting funds or changing attribution.
			for receipt := 0; receipt < workers/2; receipt++ {
				require.NoError(t, credit(fmt.Sprintf("%s:receipt:%d", account.AccountID, receipt)))
			}
			assertCanonicalWalletState(t, account.AccountID, 760, 300, 1060, 0)
			var credits, lots int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ? AND reason_code = ?", account.AccountID, "redemption_credit").Count(&credits).Error)
			require.NoError(t, db.Model(&billingschema.FundingLot{}).Where("account_id = ?", account.AccountID).Count(&lots).Error)
			require.EqualValues(t, workers/2, credits)
			require.EqualValues(t, workers/2, lots)
			// Simultaneous direct debits share the native receipt and must also
			// apply their compatibility projection exactly once.
			if finish == "settle" {
				require.NoError(t, db.Model(&user).Update("claude_quota", 50).Error)
			}
			debit := func(amount int) error {
				return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					return DebitClaudeWalletQuotaTxWithReason(tx, user.Id, amount, account.AccountID+":direct-debit", "wallet_peer_transfer_debit")
				})
			}
			debitStart, debitResults := make(chan struct{}), make(chan error, workers)
			for worker := 0; worker < workers; worker++ {
				go func() {
					<-debitStart
					debitResults <- debit(100)
				}()
			}
			close(debitStart)
			for worker := 0; worker < workers; worker++ {
				require.NoError(t, <-debitResults)
			}
			require.NoError(t, debit(100))
			require.ErrorIs(t, debit(101), billingdomain.ErrLedgerConflict)
			assertCanonicalWalletState(t, account.AccountID, 660, 300, 1060, 100)
			var savedUser identityschema.User
			require.NoError(t, db.First(&savedUser, user.Id).Error)
			if finish == "settle" {
				require.Zero(t, savedUser.ClaudeQuota)
			} else {
				require.Equal(t, 960, savedUser.ClaudeQuota)
			}
			err = db.Transaction(func(tx *gorm.DB) error {
				if err := DebitClaudeWalletQuotaTxWithReason(tx, user.Id, 100, account.AccountID+":debit-rollback", "wallet_peer_transfer_debit"); err != nil {
					return err
				}
				return rollback
			})
			require.ErrorIs(t, err, rollback)
			assertCanonicalWalletState(t, account.AccountID, 660, 300, 1060, 100)
			if finish == "release" {
				_, err = billingdomain.ReleaseReservation(billingdomain.ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: account.AccountID + ":release"})
				require.NoError(t, err)
				assertCanonicalWalletState(t, account.AccountID, 960, 0, 1060, 100)
			} else {
				_, err = billingdomain.SettleReservation(billingdomain.SettleReservationParams{ReservationID: reservation.ReservationID, ActualAmount: 120, IdempotencyKey: account.AccountID + ":settle"})
				require.NoError(t, err)
				assertCanonicalWalletState(t, account.AccountID, 840, 0, 1060, 220)
			}
		})
	}
	t.Run("concurrent_credit_integer_limit", func(t *testing.T) {
		account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "wallet", OwnerType: "user", OwnerID: time.Now().UnixNano()})
		require.NoError(t, err)
		_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: math.MaxInt64 - 1, IdempotencyKey: account.AccountID + ":fund"})
		require.NoError(t, err)
		const workers = 8
		start, results := make(chan struct{}), make(chan error, workers)
		for worker := 0; worker < workers; worker++ {
			key := fmt.Sprintf("%s:edge:%d", account.AccountID, worker)
			go func() {
				<-start
				_, err := billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1, IdempotencyKey: key})
				results <- err
			}()
		}
		close(start)
		accepted := 0
		for worker := 0; worker < workers; worker++ {
			if err := <-results; err == nil {
				accepted++
			} else {
				require.ErrorContains(t, err, "credit exceeds integer limit")
			}
		}
		require.Equal(t, 1, accepted)
		assertCanonicalWalletState(t, account.AccountID, math.MaxInt64, 0, math.MaxInt64, 0)
		var grants int64
		require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ?", account.AccountID).Count(&grants).Error)
		require.EqualValues(t, 2, grants)
	})
}
