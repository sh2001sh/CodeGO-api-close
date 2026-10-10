package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/dto"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func walletSequenceRequest(ctx context.Context, userID int, requestID string) (*gin.Context, *relaycommon.RelayInfo) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	return c, &relaycommon.RelayInfo{UserId: userID, RequestId: requestID, IsPlayground: true,
		ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
}

func walletSequenceDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wallet.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	require.NoError(t, database.Exec("PRAGMA journal_mode=WAL").Error)
	require.NoError(t, database.AutoMigrate(&identityschema.User{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}))
	original := platformdb.DB
	platformdb.DB = database
	t.Cleanup(func() { platformdb.DB = original; _ = sqlDB.Close() })
	return database
}

func TestWalletQueueTraceSuccessfulReservationExcludesRefundWait(t *testing.T) {
	walletSequenceDatabase(t)
	const userID = 1420
	seedUser(t, userID, 10000)
	_, err := ensureMirroredUserAccount(userID, billingAccountTypeClaudeWallet, 10000)
	require.NoError(t, err)
	unlock, err := waitRelayWallet(context.Background(), userID)
	require.NoError(t, err)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(unlock) }
	t.Cleanup(release)
	c, info := walletSequenceRequest(context.Background(), userID, "trace-success")
	info.FirstByteTrace = relaycommon.NewFirstByteTrace(time.Now())
	type result struct {
		session *BillingSession
		err     error
	}
	done := make(chan result, 1)
	go func() {
		session, apiErr := newWalletBillingSession(c, info, 100)
		if apiErr != nil {
			done <- result{err: apiErr}
		} else {
			done <- result{session: session}
		}
	}()
	require.Eventually(t, func() bool {
		relayWalletSequence.Lock()
		defer relayWalletSequence.Unlock()
		return relayWalletSequence.accounts[userID] != nil && relayWalletSequence.accounts[userID].references == 2
	}, time.Second, time.Millisecond)
	time.Sleep(15 * time.Millisecond)
	release()
	var session *BillingSession
	select {
	case res := <-done:
		require.NoError(t, res.err)
		session = res.session
	case <-time.After(time.Second):
		t.Fatal("reservation did not leave the wallet queue")
	}
	require.NotNil(t, session)
	waitMs := info.FirstByteTrace.ProgressSnapshot(time.Now())["wallet_queue_wait_ms"]
	require.GreaterOrEqual(t, waitMs, int64(15))
	funding := session.funding.(*LedgerRelayFunding)
	require.True(t, funding.walletQueueWaitObserved)
	require.Equal(t, funding.walletQueueWaitDuration.Milliseconds(), waitMs)
	// A separate, contended completion queue must not enter the first-reserve timer.
	unlockRefund, err := waitRelayWallet(context.Background(), userID)
	require.NoError(t, err)
	var refundOnce sync.Once
	releaseRefund := func() { refundOnce.Do(unlockRefund) }
	t.Cleanup(releaseRefund)
	refundDone := make(chan error, 1)
	go func() { refundDone <- session.RefundSync(c) }()
	require.Eventually(t, func() bool {
		relayWalletSequence.Lock()
		defer relayWalletSequence.Unlock()
		return relayWalletSequence.accounts[userID] != nil && relayWalletSequence.accounts[userID].references == 2
	}, time.Second, time.Millisecond)
	time.Sleep(15 * time.Millisecond)
	releaseRefund()
	select {
	case err := <-refundDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("refund did not leave the wallet queue")
	}
	require.Equal(t, waitMs, info.FirstByteTrace.ProgressSnapshot(time.Now())["wallet_queue_wait_ms"])
	require.EqualValues(t, 10000, loadBillingSnapshot(t, userID, billingAccountTypeClaudeWallet).AvailableBalance)
}

func TestWalletQueueTraceCancelledReservationPreservesWait(t *testing.T) {
	walletSequenceDatabase(t)
	const userID = 1421
	seedUser(t, userID, 10000)
	_, err := ensureMirroredUserAccount(userID, billingAccountTypeClaudeWallet, 10000)
	require.NoError(t, err)
	unlock, err := waitRelayWallet(context.Background(), userID)
	require.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, info := walletSequenceRequest(ctx, userID, "trace-cancelled")
	info.FirstByteTrace = relaycommon.NewFirstByteTrace(time.Now())
	info.FirstByteTrace.MarkBodyReadDone()
	done := make(chan error, 1)
	go func() {
		_, apiErr := newWalletBillingSession(c, info, 100)
		if apiErr != nil {
			done <- apiErr
		} else {
			done <- nil
		}
	}()
	require.Eventually(t, func() bool {
		relayWalletSequence.Lock()
		defer relayWalletSequence.Unlock()
		return relayWalletSequence.accounts[userID] != nil && relayWalletSequence.accounts[userID].references == 2
	}, time.Second, time.Millisecond)
	time.Sleep(15 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancelled reservation retained its queue waiter")
	}
	trace := info.FirstByteTrace.ProgressSnapshot(time.Now())
	require.GreaterOrEqual(t, trace["wallet_queue_wait_ms"], int64(15))
	require.EqualValues(t, 1, trace["body_read_complete"])
	require.Zero(t, trace["upstream_request_started"])
	snapshot := loadBillingSnapshot(t, userID, billingAccountTypeClaudeWallet)
	require.EqualValues(t, 10000, snapshot.AvailableBalance)
	require.Zero(t, snapshot.ReservedBalance)
}

func TestWalletBillingHotQueueKeepsSmallPoolUsable(t *testing.T) {
	database := walletSequenceDatabase(t)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	for _, id := range []int{1401, 1402} {
		require.NoError(t, database.Create(&identityschema.User{Id: id, Username: fmt.Sprintf("sequence-%d", id), AffCode: fmt.Sprintf("seq-%d", id), ClaudeQuota: 10000}).Error)
		_, err := ensureMirroredUserAccount(id, billingAccountTypeClaudeWallet, 10000)
		require.NoError(t, err)
	}

	// Hold the first real transaction after Begin has leased its SQL connection,
	// before its first query. This leaves the other connection usable by unrelated work.
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, database.Callback().Query().Before("gorm:query").Register("test:hold_first_wallet_transaction", func(tx *gorm.DB) {
		if tx.Statement.Table == "billing_reservations" {
			if _, inTransaction := tx.Statement.ConnPool.(*sql.Tx); inTransaction {
				if strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"].Expression), "sequence-first:reserve") {
					once.Do(func() { close(entered); <-release })
				}
			}
		}
	}))
	t.Cleanup(func() { _ = database.Callback().Query().Remove("test:hold_first_wallet_transaction") })
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	firstDone := make(chan error, 1)
	go func() {
		c, info := walletSequenceRequest(context.Background(), 1401, "sequence-first")
		_, apiErr := NewBillingSession(c, info, 100)
		if apiErr != nil {
			firstDone <- apiErr
		} else {
			firstDone <- nil
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first wallet transaction did not start")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queued := make(chan error, 12)
	for i := 0; i < cap(queued); i++ {
		go func(i int) {
			c, info := walletSequenceRequest(ctx, 1401, fmt.Sprintf("sequence-cancel-%d", i))
			_, apiErr := NewBillingSession(c, info, 100)
			if apiErr != nil {
				queued <- apiErr.Unwrap()
			} else {
				queued <- nil
			}
		}(i)
	}
	// Cancellation must finish all queued requests while the first transaction
	// is still blocked, rather than leaving BeginTx behind the SQL pool.
	time.Sleep(30 * time.Millisecond)
	cancel()
	for i := 0; i < cap(queued); i++ {
		select {
		case err := <-queued:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(2 * time.Second):
			t.Fatal("cancelled reservation retained a pool waiter")
		}
	}
	require.Equal(t, 1, sqlDB.Stats().InUse)
	otherDone := make(chan error, 1)
	go func() {
		c, info := walletSequenceRequest(context.Background(), 1402, "sequence-other")
		_, apiErr := NewBillingSession(c, info, 200)
		if apiErr != nil {
			otherDone <- apiErr
		} else {
			otherDone <- nil
		}
	}()
	select {
	case err := <-otherDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("unrelated wallet could not use the other connection")
	}
	queryCtx, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	var user identityschema.User
	require.NoError(t, database.WithContext(queryCtx).First(&user, 1402).Error)
	unblock()
	select {
	case err := <-firstDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("first reservation failed to finish")
	}
	var cancelled int64
	require.NoError(t, database.Model(&billingschema.BillingReservation{}).Where("request_id LIKE ?", "sequence-cancel-%").Count(&cancelled).Error)
	require.Zero(t, cancelled)
	require.EqualValues(t, 9900, loadBillingSnapshot(t, 1401, billingAccountTypeClaudeWallet).AvailableBalance)
	require.EqualValues(t, 9800, loadBillingSnapshot(t, 1402, billingAccountTypeClaudeWallet).AvailableBalance)
	relayWalletSequence.Lock()
	remaining := len(relayWalletSequence.accounts)
	relayWalletSequence.Unlock()
	require.Zero(t, remaining)
}

func TestWalletBillingCancellationLeavesSaturatedPoolLookup(t *testing.T) {
	database := walletSequenceDatabase(t)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	first, err := sqlDB.Conn(context.Background())
	require.NoError(t, err)
	defer first.Close()
	second, err := sqlDB.Conn(context.Background())
	require.NoError(t, err)
	defer second.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		c, info := walletSequenceRequest(ctx, 1407, "sequence-lookup-cancel")
		_, apiErr := NewBillingSession(c, info, 100)
		if apiErr == nil {
			finished <- nil
		} else {
			finished <- apiErr.Unwrap()
		}
	}()
	deadline := time.Now().Add(time.Second)
	for sqlDB.Stats().WaitCount == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	require.Positive(t, sqlDB.Stats().WaitCount)
	cancel()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancelled wallet lookup retained a SQL pool waiter")
	}
}

func TestWalletBillingCancellationRollsBackActiveReservation(t *testing.T) {
	database := walletSequenceDatabase(t)
	seedUser(t, 1408, 10000)
	_, err := ensureMirroredUserAccount(1408, billingAccountTypeClaudeWallet, 10000)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:cancel_active_reservation", func(tx *gorm.DB) {
		if tx.Statement.Table == "billing_balance_snapshots" {
			if _, inTransaction := tx.Statement.ConnPool.(*sql.Tx); inTransaction {
				cancel()
			}
		}
	}))
	c, info := walletSequenceRequest(ctx, 1408, "sequence-active-cancel")
	_, apiErr := NewBillingSession(c, info, 100)
	require.ErrorIs(t, apiErr, context.Canceled)
	require.NoError(t, database.Callback().Query().Remove("test:cancel_active_reservation"))
	snapshot := loadBillingSnapshot(t, 1408, billingAccountTypeClaudeWallet)
	require.EqualValues(t, 10000, snapshot.AvailableBalance)
	require.Zero(t, snapshot.ReservedBalance)
	var reservations int64
	require.NoError(t, database.Model(&billingschema.BillingReservation{}).Count(&reservations).Error)
	require.Zero(t, reservations)
}

func TestWalletBillingCompletionSurvivesRequestCancellation(t *testing.T) {
	for i, operation := range []string{"settle", "refund"} {
		t.Run(operation, func(t *testing.T) {
			truncate(t)
			userID := 1403 + i
			seedUser(t, userID, 10000)
			ctx, cancel := context.WithCancel(context.Background())
			c, info := walletSequenceRequest(ctx, userID, "sequence-completion-"+operation)
			session, apiErr := NewBillingSession(c, info, 300)
			require.Nil(t, apiErr)
			cancel()
			// A retry must stop, but completed usage and its larger final charge
			// must still settle after the HTTP request has ended.
			require.ErrorIs(t, session.Reserve(500), context.Canceled)
			if operation == "settle" {
				require.NoError(t, session.Settle(450))
				require.NoError(t, session.Settle(450))
				require.EqualValues(t, 9550, loadBillingSnapshot(t, userID, billingAccountTypeClaudeWallet).AvailableBalance)
			} else {
				require.NoError(t, session.RefundSync(c))
				require.NoError(t, session.RefundSync(c))
				require.EqualValues(t, 10000, loadBillingSnapshot(t, userID, billingAccountTypeClaudeWallet).AvailableBalance)
			}
			require.Zero(t, loadBillingSnapshot(t, userID, billingAccountTypeClaudeWallet).ReservedBalance)
		})
	}
}

func TestWalletBillingReservationReplayAndConflictRemainExact(t *testing.T) {
	truncate(t)
	seedUser(t, 1405, 10000)
	first, err := NewLedgerRelayFunding(1405, "sequence-replay", BillingSourceWallet)
	require.NoError(t, err)
	require.NoError(t, first.PreConsume(300))
	second, err := NewLedgerRelayFunding(1405, "sequence-replay", BillingSourceWallet)
	require.NoError(t, err)
	require.NoError(t, second.PreConsume(300))
	require.Equal(t, first.ReservationID(), second.ReservationID())
	require.ErrorIs(t, second.PreConsume(301), billingdomain.ErrLedgerConflict)
	conflicting, err := NewLedgerRelayFunding(1405, "sequence-replay", BillingSourceWallet)
	require.NoError(t, err)
	require.ErrorIs(t, conflicting.PreConsume(301), billingdomain.ErrLedgerConflict)
	require.EqualValues(t, 9700, loadBillingSnapshot(t, 1405, billingAccountTypeClaudeWallet).AvailableBalance)
}

func TestWalletBillingTrustedCompletionSurvivesCancellation(t *testing.T) {
	truncate(t)
	seedUser(t, 1406, 10000)
	funding, err := NewLedgerRelayFunding(1406, "sequence-trusted-completion", BillingSourceWallet)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	funding.requestContext = ctx
	cancel()
	session := &BillingSession{relayInfo: &relaycommon.RelayInfo{IsPlayground: true}, funding: funding, trusted: true}
	require.NoError(t, session.Settle(450))
	require.NoError(t, session.Settle(450))
	require.EqualValues(t, 9550, loadBillingSnapshot(t, 1406, billingAccountTypeClaudeWallet).AvailableBalance)
	require.Zero(t, loadBillingSnapshot(t, 1406, billingAccountTypeClaudeWallet).ReservedBalance)
}
