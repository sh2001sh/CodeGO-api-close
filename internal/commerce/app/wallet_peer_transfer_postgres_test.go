package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func TestWalletTransferConcurrentReservedBalancePostgres(t *testing.T) {
	dsn := os.Getenv("WALLET_CANONICAL_TEST_DSN")
	if dsn == "" {
		t.Skip("WALLET_CANONICAL_TEST_DSN requires a dedicated codego_wallet_test_ database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var database string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&database).Error)
	require.True(t, strings.HasPrefix(database, "codego_wallet_test_"), "dedicated wallet test database required")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(20)
	previousDB, previousLogDB := platformdb.DB, platformdb.LogDB
	previousPG, previousSQLite, previousMySQL, previousRedis := platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	platformdb.DB, platformdb.LogDB = db.WithContext(ctx), db.WithContext(ctx)
	platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = true, false, false, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB = previousDB, previousLogDB
		platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = previousPG, previousSQLite, previousMySQL, previousRedis
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &auditschema.Log{}, &commerceschema.WalletTransfer{}, &commerceschema.WalletTransferSecurity{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}, &billingschema.FundingLot{}, &billingschema.FundingAllocation{}))

	for _, unique := range []int{1, 4, 8} {
		t.Run(fmt.Sprintf("unique_receipts_%d", unique), func(t *testing.T) {
			identity := fmt.Sprintf("xferpg%d", time.Now().UnixNano())
			senderExternalID, err := identityschema.GenerateExternalUserID()
			require.NoError(t, err)
			recipientExternalID, err := identityschema.GenerateExternalUserID()
			require.NoError(t, err)
			sender := identityschema.User{Username: identity, ExternalId: senderExternalID, AffCode: identity, Status: constant.UserStatusEnabled}
			recipient := identityschema.User{Username: identity + "r", ExternalId: recipientExternalID, AffCode: identity + "r", Status: constant.UserStatusEnabled}
			require.NoError(t, db.Create(&sender).Error)
			require.NoError(t, db.Create(&recipient).Error)
			require.NoError(t, ConfigureWalletTransferPassword(sender.Id, "", "Paypass123"))
			amount := int64(platformruntime.QuotaPerUnit)
			initial, reserved := amount*100, amount*40
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				return billingapp.CreditClaudeWalletQuotaTx(tx, sender.Id, int(initial), identity+":fund", "redemption_credit")
			}))
			var account billingschema.BillingAccount
			require.NoError(t, db.Where("owner_id = ? AND account_type = ?", sender.Id, "claude_wallet").First(&account).Error)
			reservation, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, RequestID: identity, ReservedAmount: reserved, IdempotencyKey: identity + ":reserve"})
			require.NoError(t, err)
			blocker := db.Begin()
			require.NoError(t, blocker.Error)
			t.Cleanup(func() { _ = blocker.Rollback().Error })
			var locked identityschema.User
			require.NoError(t, blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, sender.Id).Error)
			var blockerPID int
			require.NoError(t, blocker.Raw("SELECT pg_backend_pid()").Scan(&blockerPID).Error)
			const workers = 8
			start, results := make(chan struct{}), make(chan error, workers)
			for worker := 0; worker < workers; worker++ {
				request := CreateWalletTransferRequest{RecipientExternalId: recipient.ExternalId, AmountQuota: amount, PaymentPassword: "Paypass123", RequestId: fmt.Sprintf("%s:receipt:%d", identity, worker%unique)}
				go func() {
					<-start
					_, err := TransferWalletQuota(sender.Id, request)
					results <- err
				}()
			}
			close(start)
			deadline := time.Now().Add(5 * time.Second)
			for {
				var waiting int64
				require.NoError(t, db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid))", blockerPID).Scan(&waiting).Error)
				if waiting > 0 {
					t.Logf("verified PostgreSQL transfer lock contention: %d independent waiters", waiting)
					break
				}
				require.True(t, time.Now().Before(deadline), "no independent transfer transaction waited for the user lock")
				time.Sleep(10 * time.Millisecond)
			}
			require.NoError(t, blocker.Commit().Error)
			for worker := 0; worker < workers; worker++ {
				require.NoError(t, <-results)
			}
			feeTotal, principalTotal := int64(unique)*walletTransferFee(amount), int64(unique)*amount
			var senderSnapshot, recipientSnapshot billingschema.BillingBalanceSnapshot
			require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&senderSnapshot).Error)
			var recipientAccount billingschema.BillingAccount
			require.NoError(t, db.Where("owner_id = ? AND account_type = ?", recipient.Id, "claude_wallet").First(&recipientAccount).Error)
			require.NoError(t, db.Where("account_id = ?", recipientAccount.AccountID).First(&recipientSnapshot).Error)
			require.Equal(t, initial, senderSnapshot.GrantedTotal)
			require.Equal(t, reserved, senderSnapshot.ReservedBalance)
			require.Equal(t, principalTotal+feeTotal, senderSnapshot.ConsumedTotal)
			require.Equal(t, initial-reserved-principalTotal-feeTotal, senderSnapshot.AvailableBalance)
			require.Equal(t, principalTotal, recipientSnapshot.AvailableBalance)
			require.Equal(t, principalTotal, recipientSnapshot.GrantedTotal)
			require.Equal(t, initial, senderSnapshot.AvailableBalance+senderSnapshot.ReservedBalance+recipientSnapshot.AvailableBalance+feeTotal)
			var transfers, credits, reconciles int64
			require.NoError(t, db.Model(&commerceschema.WalletTransfer{}).Where("sender_user_id = ?", sender.Id).Count(&transfers).Error)
			require.EqualValues(t, unique, transfers)
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ? AND reason_code = ?", recipientAccount.AccountID, "wallet_peer_transfer_credit").Count(&credits).Error)
			require.EqualValues(t, unique, credits)
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ? AND reason_code = ?", account.AccountID, "ledger_sync_reconcile").Count(&reconciles).Error)
			require.Zero(t, reconciles)
			_, err = TransferWalletQuota(sender.Id, CreateWalletTransferRequest{RecipientExternalId: recipient.ExternalId, AmountQuota: initial - reserved, PaymentPassword: "Paypass123", RequestId: identity + ":overspend"})
			require.ErrorIs(t, err, commerceschema.ErrWalletTransferInsufficientBalance)
			_, err = billingdomain.ReleaseReservation(billingdomain.ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: identity + ":release"})
			require.NoError(t, err)
			require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&senderSnapshot).Error)
			require.Equal(t, initial-principalTotal-feeTotal, senderSnapshot.AvailableBalance)
			require.Zero(t, senderSnapshot.ReservedBalance)
		})
	}
}
