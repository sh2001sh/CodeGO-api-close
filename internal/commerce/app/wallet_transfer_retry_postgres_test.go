package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type walletTransferRetryContextKey struct{}

func seedWalletTransferRetryPostgres(t *testing.T, db *gorm.DB) (identityschema.User, identityschema.User, billingschema.BillingAccount, billingschema.BillingAccount, CreateWalletTransferRequest) {
	t.Helper()
	identity := fmt.Sprintf("retry%d", time.Now().UnixNano())
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
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := billingapp.CreditClaudeWalletQuotaTx(tx, sender.Id, int(amount*10), identity+":fund-sender", "redemption_credit"); err != nil {
			return err
		}
		return billingapp.CreditClaudeWalletQuotaTx(tx, recipient.Id, int(amount*2), identity+":fund-recipient", "redemption_credit")
	}))
	var senderAccount, recipientAccount billingschema.BillingAccount
	require.NoError(t, db.Where("owner_id = ? AND account_type = ?", sender.Id, "claude_wallet").First(&senderAccount).Error)
	require.NoError(t, db.Where("owner_id = ? AND account_type = ?", recipient.Id, "claude_wallet").First(&recipientAccount).Error)
	request := CreateWalletTransferRequest{RecipientExternalId: recipient.ExternalId, AmountQuota: amount, PaymentPassword: "Paypass123", RequestId: identity + ":transfer"}
	return sender, recipient, senderAccount, recipientAccount, request
}

func assertWalletTransferRetryPostgres(t *testing.T, db *gorm.DB, senderAccount, recipientAccount billingschema.BillingAccount, request CreateWalletTransferRequest) {
	t.Helper()
	var sender, recipient billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", senderAccount.AccountID).First(&sender).Error)
	require.NoError(t, db.Where("account_id = ?", recipientAccount.AccountID).First(&recipient).Error)
	fee := walletTransferFee(request.AmountQuota)
	require.Equal(t, request.AmountQuota*10-request.AmountQuota-fee, sender.AvailableBalance)
	require.Equal(t, request.AmountQuota+fee, sender.ConsumedTotal)
	require.Zero(t, sender.ReservedBalance)
	require.Equal(t, request.AmountQuota*3, recipient.AvailableBalance)
	var receipts, credits, debits int64
	require.NoError(t, db.Model(&commerceschema.WalletTransfer{}).Where("request_id = ?", request.RequestId).Count(&receipts).Error)
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", "wallet-transfer:"+request.RequestId+":credit").Count(&credits).Error)
	require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Where("idempotency_key IN ?", []string{"ledger-settlement:wallet-transfer:" + request.RequestId + ":debit", "ledger-settlement:wallet-transfer:" + request.RequestId + ":fee"}).Count(&debits).Error)
	require.EqualValues(t, 1, receipts)
	require.EqualValues(t, 1, credits)
	require.EqualValues(t, 2, debits)
}

func TestWalletTransferRetriesRealDeadlockPostgres(t *testing.T) {
	db := openWalletTransferPostgresTestDB(t)
	sender, recipient, senderAccount, recipientAccount, request := seedWalletTransferRetryPostgres(t, db)
	var attempts, passwordReads, deadlocks atomic.Int32
	var deadlines []time.Time
	var beforeRetry billingschema.BillingBalanceSnapshot
	callback := "test:wallet_transfer_deadlock"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(walletTransferRetryContextKey{}) != true {
			return
		}
		if tx.Statement.Table == "wallet_transfer_securities" {
			passwordReads.Add(1)
		}
		if tx.Statement.Table == "users" && strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"].Expression), "external_id") {
			attempt := attempts.Add(1)
			deadline, ok := tx.Statement.Context.Deadline()
			require.True(t, ok)
			deadlines = append(deadlines, deadline)
			if err := tx.Exec("SET LOCAL deadlock_timeout = '250ms'").Error; err != nil {
				tx.AddError(err)
			}
			if attempt == 2 {
				require.NoError(t, db.Where("account_id = ?", senderAccount.AccountID).First(&beforeRetry).Error)
				var allocations int64
				require.NoError(t, db.Model(&billingschema.FundingAllocation{}).Where("account_id = ?", senderAccount.AccountID).Count(&allocations).Error)
				require.Zero(t, allocations, "the deadlock must roll back original sender source allocations")
			}
		}
	}))
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback+":error", func(tx *gorm.DB) {
		var pgError *pgconn.PgError
		if tx.Statement.Context.Value(walletTransferRetryContextKey{}) == true && errors.As(tx.Error, &pgError) && pgError.Code == "40P01" {
			deadlocks.Add(1)
			t.Logf("observed actual PostgreSQL rollback: SQLSTATE %s", pgError.Code)
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callback)
		_ = db.Callback().Query().Remove(callback + ":error")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	blocker := db.WithContext(ctx).Begin()
	require.NoError(t, blocker.Error)
	t.Cleanup(func() { _ = blocker.Rollback().Error })
	require.NoError(t, blocker.Exec("SET LOCAL deadlock_timeout = '5s'").Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, blocker.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", recipientAccount.AccountID).First(&snapshot).Error)
	var blockerPID int
	require.NoError(t, blocker.Raw("SELECT pg_backend_pid()").Scan(&blockerPID).Error)
	type result struct {
		item *WalletTransferHistoryItem
		err  error
	}
	completed := make(chan result, 1)
	started := time.Now()
	go func() {
		item, err := TransferWalletQuota(context.WithValue(context.Background(), walletTransferRetryContextKey{}, true), sender.Id, request)
		completed <- result{item, err}
	}()
	require.Eventually(t, func() bool {
		var waiting int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid))", blockerPID).Scan(&waiting).Error)
		return waiting > 0
	}, 3*time.Second, 5*time.Millisecond)
	// This completes the real cycle after the transfer has already debited its
	// sender: transfer holds users and waits for the recipient snapshot, while
	// this writer holds that snapshot and waits for the recipient user.
	var user identityschema.User
	require.NoError(t, blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, recipient.Id).Error)
	require.NoError(t, blocker.Commit().Error)
	finished := <-completed
	t.Logf("transfer attempts=%d password checks=%d deadlocks=%d elapsed=%s", attempts.Load(), passwordReads.Load(), deadlocks.Load(), time.Since(started))
	require.NoError(t, finished.err)
	require.EqualValues(t, 2, attempts.Load())
	require.EqualValues(t, 1, passwordReads.Load())
	require.GreaterOrEqual(t, deadlocks.Load(), int32(1))
	require.Len(t, deadlines, 2)
	require.Equal(t, deadlines[0], deadlines[1], "retry must preserve the original total deadline")
	require.LessOrEqual(t, deadlines[0].Sub(started), WalletTransferTimeout+time.Second)
	require.Equal(t, request.AmountQuota*10, beforeRetry.AvailableBalance)
	require.Zero(t, beforeRetry.ConsumedTotal)
	assertWalletTransferRetryPostgres(t, db, senderAccount, recipientAccount, request)
	replayed, err := TransferWalletQuota(context.Background(), sender.Id, request)
	require.NoError(t, err)
	require.Equal(t, finished.item.Id, replayed.Id)
	assertWalletTransferRetryPostgres(t, db, senderAccount, recipientAccount, request)
}

func TestWalletTransferRetriesRealSerializationFailurePostgres(t *testing.T) {
	db := openWalletTransferPostgresTestDB(t)
	sender, recipient, senderAccount, recipientAccount, request := seedWalletTransferRetryPostgres(t, db)
	var attempts, passwordReads, serializationFailures atomic.Int32
	callback := "test:wallet_transfer_serialization"
	isTransferRead := func(tx *gorm.DB) bool {
		return tx.Statement.Context.Value(walletTransferRetryContextKey{}) == true && tx.Statement.Table == "users" && strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"].Expression), "external_id")
	}
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(walletTransferRetryContextKey{}) == true && tx.Statement.Table == "wallet_transfer_securities" {
			passwordReads.Add(1)
		}
		if isTransferRead(tx) {
			attempts.Add(1)
			if err := tx.Exec("SET TRANSACTION ISOLATION LEVEL SERIALIZABLE").Error; err != nil {
				tx.AddError(err)
			}
		}
	}))
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback+":compete", func(tx *gorm.DB) {
		if isTransferRead(tx) && attempts.Load() == 1 && tx.Error == nil {
			// A second connection commits after this transaction's MVCC read.
			// Its following FOR UPDATE must produce a real SQLSTATE 40001.
			require.NoError(t, db.Model(&recipient).UpdateColumn("display_name", "concurrent display").Error)
		}
		var pgError *pgconn.PgError
		if tx.Statement.Context.Value(walletTransferRetryContextKey{}) == true && errors.As(tx.Error, &pgError) && pgError.Code == "40001" {
			serializationFailures.Add(1)
			t.Logf("observed actual PostgreSQL rollback: SQLSTATE %s", pgError.Code)
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callback)
		_ = db.Callback().Query().Remove(callback + ":compete")
	})
	created, err := TransferWalletQuota(context.WithValue(context.Background(), walletTransferRetryContextKey{}, true), sender.Id, request)
	require.NoError(t, err)
	require.EqualValues(t, 2, attempts.Load())
	require.EqualValues(t, 1, passwordReads.Load())
	require.GreaterOrEqual(t, serializationFailures.Load(), int32(1))
	assertWalletTransferRetryPostgres(t, db, senderAccount, recipientAccount, request)
	replayed, err := TransferWalletQuota(context.Background(), sender.Id, request)
	require.NoError(t, err)
	require.Equal(t, created.Id, replayed.Id)
	assertWalletTransferRetryPostgres(t, db, senderAccount, recipientAccount, request)
}

type walletTransferUnknownCommitPool struct {
	*sql.DB
	commits atomic.Int32
}

func (pool *walletTransferUnknownCommitPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := pool.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &walletTransferUnknownCommitTx{Tx: tx, pool: pool}, nil
}

type walletTransferUnknownCommitTx struct {
	*sql.Tx
	pool *walletTransferUnknownCommitPool
}

func (tx *walletTransferUnknownCommitTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.pool.commits.Add(1) == 2 {
		// Password verification committed first. The financial transaction now
		// commits successfully, but its acknowledgement is deliberately lost.
		return io.EOF
	}
	return nil
}

func TestWalletTransferUnknownCommitDoesNotRetryPostgres(t *testing.T) {
	db := openWalletTransferPostgresTestDB(t)
	sender, _, senderAccount, recipientAccount, request := seedWalletTransferRetryPostgres(t, db)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	pool := &walletTransferUnknownCommitPool{DB: sqlDB}
	wrapped := db.Session(&gorm.Session{Context: context.Background()})
	wrapped.Statement.ConnPool = pool
	wrapped.Config.ConnPool = pool
	previousDB := platformdb.DB
	platformdb.DB = wrapped
	t.Cleanup(func() { platformdb.DB = previousDB })
	_, err = TransferWalletQuota(context.Background(), sender.Id, request)
	require.ErrorIs(t, err, io.EOF)
	require.ErrorIs(t, err, commerceschema.ErrWalletTransferUnavailable)
	require.EqualValues(t, 2, pool.commits.Load(), "ambiguous financial commit must not start another transaction")
	assertWalletTransferRetryPostgres(t, db, senderAccount, recipientAccount, request)
	platformdb.DB = previousDB
	_, err = TransferWalletQuota(context.Background(), sender.Id, request)
	require.NoError(t, err, "the original receipt safely resolves a lost acknowledgement")
	assertWalletTransferRetryPostgres(t, db, senderAccount, recipientAccount, request)
	t.Log("real financial COMMIT succeeded; injected EOF returned unconfirmed with no automatic replay; same receipt resolved once")
}

func TestWalletTransferRetryBudgetAndCancellationPostgres(t *testing.T) {
	db := openWalletTransferPostgresTestDB(t)
	sender, _, _, _, _ := seedWalletTransferRetryPostgres(t, db)
	for _, code := range []string{"40P01", "40001", "55P03", "23505"} {
		t.Run(code, func(t *testing.T) {
			var before, after identityschema.User
			require.NoError(t, db.First(&before, sender.Id).Error)
			attempts := 0
			err := runWalletTransferTransaction(context.Background(), db, func(tx *gorm.DB) error {
				attempts++
				if err := tx.Model(&sender).UpdateColumn("claude_quota", gorm.Expr("claude_quota + 1")).Error; err != nil {
					return err
				}
				// The code is a fixed test case, never untrusted input.
				return tx.Exec(fmt.Sprintf("DO $retry$ BEGIN RAISE EXCEPTION 'forced rollback' USING ERRCODE = '%s'; END $retry$", code)).Error
			})
			var pgError *pgconn.PgError
			require.ErrorAs(t, err, &pgError)
			require.Equal(t, code, pgError.Code)
			if code == "40P01" || code == "40001" {
				require.Equal(t, walletTransferMaxAttempts, attempts)
			} else {
				require.Equal(t, 1, attempts)
			}
			require.NoError(t, db.First(&after, sender.Id).Error)
			require.Equal(t, before.ClaudeQuota, after.ClaudeQuota, "all failed attempts must roll back their writes")
		})
	}
	t.Run("cancel_backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		attempts := 0
		firstFailed := make(chan struct{})
		go func() {
			<-firstFailed
			time.Sleep(5 * time.Millisecond)
			cancel()
		}()
		started := time.Now()
		err := runWalletTransferTransaction(ctx, db, func(tx *gorm.DB) error {
			attempts++
			err := tx.Exec("DO $retry$ BEGIN RAISE EXCEPTION 'forced rollback' USING ERRCODE = '40001'; END $retry$").Error
			close(firstFailed)
			return err
		})
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, attempts)
		require.Less(t, time.Since(started), time.Second)
	})
}
