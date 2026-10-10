package domain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type reservationPostgresSQLCounter struct {
	logger.Interface
	queries atomic.Int64
}

func (counter *reservationPostgresSQLCounter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	counter.queries.Add(1)
	counter.Interface.Trace(ctx, begin, fc, err)
}

func TestReservationPostgresFastPath(t *testing.T) {
	dsn := os.Getenv("TEST_LEDGER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("real PostgreSQL fixture not configured: set TEST_LEDGER_POSTGRES_DSN; SQLite cannot validate this fast path")
	}
	counter := &reservationPostgresSQLCounter{Interface: logger.Default.LogMode(logger.Silent)}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: counter})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(20)
	oldDB, oldPG, oldSQLite := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = db, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = oldDB, oldPG, oldSQLite
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}))

	seed := func(t *testing.T, amount int64) string {
		t.Helper()
		account, err := EnsureBillingAccount(EnsureAccountParams{AccountType: "claude_wallet", OwnerType: "user", OwnerID: time.Now().UnixNano()})
		require.NoError(t, err)
		_, err = CreditAccount(CreditAccountParams{AccountID: account.AccountID, Amount: amount, IdempotencyKey: "seed:" + account.AccountID})
		require.NoError(t, err)
		return account.AccountID
	}
	params := func(account string, amount int64) CreateReservationParams {
		key := platformruntime.GetUUID()
		return CreateReservationParams{AccountID: account, RequestID: key, ReservedAmount: amount, IdempotencyKey: "reserve:" + key}
	}
	snapshot := func(t *testing.T, account string, available, reserved int64) {
		t.Helper()
		var value billingschema.BillingBalanceSnapshot
		require.NoError(t, db.Where("account_id = ?", account).First(&value).Error)
		require.Equal(t, available, value.AvailableBalance)
		require.Equal(t, reserved, value.ReservedBalance)
	}
	count := func(t *testing.T, model interface{}, column, value string) int64 {
		t.Helper()
		var result int64
		require.NoError(t, db.Model(model).Where(column+" = ?", value).Count(&result).Error)
		return result
	}

	t.Run("SQLRoundTripsAndPayload", func(t *testing.T) {
		account := seed(t, 1000)
		input := params(account, 250)
		expires := time.Now().UTC().Add(time.Hour)
		input.ExpiresAt = &expires
		input.WorkflowID = " workflow "
		counter.queries.Store(0)
		reservation, err := CreateReservation(input)
		require.NoError(t, err)
		require.EqualValues(t, 4, counter.queries.Load(), "normal create previously executed 8 statements, excluding BEGIN/COMMIT")
		t.Logf("create SQL statements: %d (previous normal path: 8; BEGIN/COMMIT unchanged)", counter.queries.Load())
		snapshot(t, account, 750, 250)
		var event billingschema.BillingOutboxEvent
		require.NoError(t, db.Where("idempotency_key = ?", "outbox:"+input.IdempotencyKey).First(&event).Error)
		payload, err := json.Marshal(reservation)
		require.NoError(t, err)
		require.JSONEq(t, string(payload), string(event.Payload))
		require.Equal(t, billingschema.BillingOutboxStatusPending, event.Status)
		require.Zero(t, event.Attempts)
		require.Equal(t, "workflow", reservation.WorkflowID)

		counter.queries.Store(0)
		released, err := ReleaseReservation(ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: "release:" + input.IdempotencyKey})
		require.NoError(t, err)
		require.EqualValues(t, 4, counter.queries.Load(), "normal release previously executed 9 statements, excluding BEGIN/COMMIT")
		t.Logf("release SQL statements: %d (previous normal path: 9; BEGIN/COMMIT unchanged)", counter.queries.Load())
		require.Equal(t, billingschema.BillingReservationStatusReleased, released.Status)
		snapshot(t, account, 1000, 0)
		event = billingschema.BillingOutboxEvent{}
		require.NoError(t, db.Where("idempotency_key = ?", "outbox:release:"+input.IdempotencyKey).First(&event).Error)
		payload, err = json.Marshal(released)
		require.NoError(t, err)
		require.JSONEq(t, string(payload), string(event.Payload))
		require.EqualValues(t, 3, count(t, &billingschema.BillingOutboxEvent{}, "account_id", account))
	})

	t.Run("ConcurrentSameWalletAndReplay", func(t *testing.T) {
		account := seed(t, 1000)
		input := params(account, 100)
		const workers = 12
		ids, failures := make(chan string, workers), make(chan error, workers)
		start := make(chan struct{})
		var group sync.WaitGroup
		for range workers {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				reservation, err := CreateReservation(input)
				failures <- err
				if err == nil {
					ids <- reservation.ReservationID
				}
			}()
		}
		close(start)
		group.Wait()
		close(ids)
		close(failures)
		for err := range failures {
			require.NoError(t, err)
		}
		first := ""
		for id := range ids {
			if first == "" {
				first = id
			}
			require.Equal(t, first, id)
		}
		snapshot(t, account, 900, 100)
		require.EqualValues(t, 1, count(t, &billingschema.BillingReservation{}, "account_id", account))
		require.EqualValues(t, 2, count(t, &billingschema.BillingLedgerEntry{}, "account_id", account))
		for _, field := range []string{"amount", "request", "workflow", "account"} {
			conflict := input
			switch field {
			case "amount":
				conflict.ReservedAmount++
			case "request":
				conflict.RequestID += "-changed"
			case "workflow":
				conflict.WorkflowID = "changed"
			case "account":
				conflict.AccountID = seed(t, 1000)
			}
			_, err := CreateReservation(conflict)
			require.ErrorIs(t, err, ErrLedgerConflict, field)
		}
		for range workers {
			group.Add(1)
			go func() {
				defer group.Done()
				_, err := ReleaseReservation(ReleaseReservationParams{ReservationID: first, IdempotencyKey: "release:" + input.IdempotencyKey})
				if err != nil {
					t.Errorf("concurrent release: %v", err)
				}
			}()
		}
		group.Wait()
		snapshot(t, account, 1000, 0)
		require.EqualValues(t, 3, count(t, &billingschema.BillingLedgerEntry{}, "account_id", account))
		require.EqualValues(t, 3, count(t, &billingschema.BillingOutboxEvent{}, "account_id", account))
		// Released reservations replay regardless of a later release key, and
		// reserve replay retains the released state rather than holding again.
		_, err := ReleaseReservation(ReleaseReservationParams{ReservationID: first, IdempotencyKey: "another:" + input.IdempotencyKey})
		require.NoError(t, err)
		replayed, err := CreateReservation(input)
		require.NoError(t, err)
		require.Equal(t, billingschema.BillingReservationStatusReleased, replayed.Status)
		snapshot(t, account, 1000, 0)
	})

	t.Run("CompetingReservationsCannotOverspend", func(t *testing.T) {
		account := seed(t, 70)
		const workers = 20
		inputs := make([]CreateReservationParams, workers)
		for i := range inputs {
			inputs[i] = params(account, 7)
		}
		failures := make(chan error, workers)
		var group sync.WaitGroup
		for _, input := range inputs {
			group.Add(1)
			go func(input CreateReservationParams) {
				defer group.Done()
				_, err := CreateReservation(input)
				failures <- err
			}(input)
		}
		group.Wait()
		close(failures)
		successes, insufficient := 0, 0
		for err := range failures {
			if err == nil {
				successes++
			} else {
				require.ErrorIs(t, err, ErrInsufficientBalance)
				insufficient++
			}
		}
		require.Equal(t, 10, successes)
		require.Equal(t, 10, insufficient)
		snapshot(t, account, 0, 70)
		require.EqualValues(t, 10, count(t, &billingschema.BillingReservation{}, "account_id", account))
		require.EqualValues(t, 11, count(t, &billingschema.BillingOutboxEvent{}, "account_id", account))
	})

	t.Run("RollbackSQLFailureAndOutboxConflict", func(t *testing.T) {
		account := seed(t, 100)
		for _, failure := range []string{"ledger_insert", "outbox_insert", "outbox_conflict", "caller_rollback"} {
			t.Run(failure, func(t *testing.T) {
				input := params(account, 25)
				if failure == "outbox_insert" {
					// Reservation and entry keys fit varchar(255), while the
					// outbox prefix exceeds it: the final write must roll back all.
					input.IdempotencyKey = strings.Repeat("x", 213) + platformruntime.GetUUID()
				}
				if failure == "ledger_insert" {
					require.NoError(t, db.Create(&billingschema.BillingLedgerEntry{AccountID: account, IdempotencyKey: "entry:" + input.IdempotencyKey, EntryType: LedgerEntryTypeAdjustment}).Error)
				}
				if failure == "outbox_conflict" {
					require.NoError(t, db.Create(&billingschema.BillingOutboxEvent{AccountID: account, IdempotencyKey: "outbox:" + input.IdempotencyKey, AggregateID: "different", EventType: "different"}).Error)
				}
				abort := errors.New("fixture caller rollback")
				err := db.Transaction(func(tx *gorm.DB) error {
					_, err := CreateReservationTx(tx, input)
					if err != nil {
						return err
					}
					return abort
				})
				require.Error(t, err)
				if failure == "outbox_conflict" {
					require.ErrorIs(t, err, ErrLedgerConflict)
				}
				if failure == "caller_rollback" {
					require.ErrorIs(t, err, abort)
				}
				snapshot(t, account, 100, 0)
				require.Zero(t, count(t, &billingschema.BillingReservation{}, "idempotency_key", input.IdempotencyKey))
				if failure != "ledger_insert" {
					require.Zero(t, count(t, &billingschema.BillingLedgerEntry{}, "idempotency_key", "entry:"+input.IdempotencyKey))
				}
				if failure != "outbox_conflict" {
					require.Zero(t, count(t, &billingschema.BillingOutboxEvent{}, "idempotency_key", "outbox:"+input.IdempotencyKey))
				}
			})
		}
		input := params(account, 25)
		reservation, err := CreateReservation(input)
		require.NoError(t, err)
		for _, rollback := range []string{"outbox_insert", "outbox_conflict", "caller_rollback"} {
			key := rollback + ":" + input.IdempotencyKey
			if rollback == "outbox_insert" {
				key = strings.Repeat("x", 213) + platformruntime.GetUUID()
			}
			if rollback == "outbox_conflict" {
				require.NoError(t, db.Create(&billingschema.BillingOutboxEvent{AccountID: account, IdempotencyKey: "outbox:" + key, AggregateID: "different", EventType: "different"}).Error)
			}
			abort := errors.New("fixture release rollback")
			err := db.Transaction(func(tx *gorm.DB) error {
				_, err := ReleaseReservationTx(tx, ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: key})
				if err != nil {
					return err
				}
				return abort
			})
			require.Error(t, err)
			if rollback == "outbox_conflict" {
				require.ErrorIs(t, err, ErrLedgerConflict)
			} else if rollback == "caller_rollback" {
				require.ErrorIs(t, err, abort)
			}
			snapshot(t, account, 75, 25)
			var unchanged billingschema.BillingReservation
			require.NoError(t, db.First(&unchanged, "reservation_id = ?", reservation.ReservationID).Error)
			require.Equal(t, billingschema.BillingReservationStatusOpen, unchanged.Status)
			require.Zero(t, count(t, &billingschema.BillingLedgerEntry{}, "idempotency_key", key))
			if rollback != "outbox_conflict" {
				require.Zero(t, count(t, &billingschema.BillingOutboxEvent{}, "idempotency_key", "outbox:"+key))
			}
		}
	})

	t.Run("ExistingOutboxReplayAndReleaseRecovery", func(t *testing.T) {
		account := seed(t, 100)
		input := params(account, 25)
		reservation, err := CreateReservation(input)
		require.NoError(t, err)
		key := "existing-event:" + input.IdempotencyKey
		event := billingschema.BillingOutboxEvent{
			AccountID: account, AggregateID: reservation.ReservationID, AggregateType: "reservation",
			EventType: "billing.reservation_released", IdempotencyKey: "outbox:" + key,
			Status: billingschema.BillingOutboxStatusPublished, Payload: json.RawMessage(`{"existing":true}`),
		}
		require.NoError(t, db.Create(&event).Error)
		_, err = ReleaseReservation(ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: key})
		require.NoError(t, err)
		snapshot(t, account, 100, 0)
		var preserved billingschema.BillingOutboxEvent
		require.NoError(t, db.First(&preserved, "event_id = ?", event.EventID).Error)
		require.Equal(t, billingschema.BillingOutboxStatusPublished, preserved.Status)
		require.JSONEq(t, `{"existing":true}`, string(preserved.Payload))

		// Preserve the pre-existing recovery branch: a matching release entry
		// repairs an open reservation's status without moving funds a second time.
		recovery, err := CreateReservation(params(account, 20))
		require.NoError(t, err)
		recoveryKey := "recovery:" + recovery.ReservationID
		require.NoError(t, db.Create(&billingschema.BillingLedgerEntry{
			AccountID: account, ReferenceID: recovery.ReservationID, ReferenceType: "reservation",
			EntryType: LedgerEntryTypeReserveRelease, Direction: billingschema.BillingDirectionCredit,
			Amount: 20, IdempotencyKey: recoveryKey,
		}).Error)
		require.NoError(t, db.Model(&billingschema.BillingBalanceSnapshot{}).Where("account_id = ?", account).
			Updates(map[string]interface{}{"available_balance": 100, "reserved_balance": 0}).Error)
		repaired, err := ReleaseReservation(ReleaseReservationParams{ReservationID: recovery.ReservationID, IdempotencyKey: recoveryKey})
		require.NoError(t, err)
		require.Equal(t, billingschema.BillingReservationStatusReleased, repaired.Status)
		snapshot(t, account, 100, 0)
		require.EqualValues(t, 1, count(t, &billingschema.BillingLedgerEntry{}, "idempotency_key", recoveryKey))
		require.Zero(t, count(t, &billingschema.BillingOutboxEvent{}, "idempotency_key", "outbox:"+recoveryKey))
	})

	t.Run("ReleaseStateAndLockOrder", func(t *testing.T) {
		account := seed(t, 100)
		input := params(account, 25)
		reservation, err := CreateReservation(input)
		require.NoError(t, err)
		holder := db.Begin()
		require.NoError(t, holder.Error)
		defer holder.Rollback()
		require.NoError(t, holder.Exec("SELECT reservation_id FROM billing.reservations WHERE reservation_id = ? FOR UPDATE", reservation.ReservationID).Error)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done := make(chan error, 1)
		backend := make(chan int, 1)
		go func() {
			err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var pid int
				if err := tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				backend <- pid
				_, err := ReleaseReservationTx(tx, ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: "lock-order:" + input.IdempotencyKey})
				return err
			})
			done <- err
		}()
		var pid int
		select {
		case pid = <-backend:
		case <-time.After(time.Second):
			t.Fatal("release did not acquire its fixture connection")
		}
		require.Eventually(t, func() bool {
			var blocked bool
			err := db.Raw("SELECT COALESCE((SELECT wait_event_type = 'Lock' FROM pg_stat_activity WHERE pid = ?), false)", pid).Scan(&blocked).Error
			return err == nil && blocked
		}, time.Second, 5*time.Millisecond, "release must be observed waiting on the held reservation row")
		// The blocked release must not own the balance lock before obtaining its
		// reservation lock: another reservation on this wallet must still finish.
		createCtx, stopCreate := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer stopCreate()
		var other *billingschema.BillingReservation
		err = db.WithContext(createCtx).Transaction(func(tx *gorm.DB) error {
			var err error
			other, err = CreateReservationTx(tx, params(account, 10))
			return err
		})
		require.NoError(t, err)
		require.NotNil(t, other)
		cancel()
		require.Error(t, <-done)
		require.NoError(t, holder.Rollback().Error)
		snapshot(t, account, 65, 35)
		_, err = ReleaseReservation(ReleaseReservationParams{ReservationID: "missing", IdempotencyKey: "missing"})
		require.ErrorIs(t, err, ErrReservationNotFound)
		require.NoError(t, db.Model(reservation).Update("status", billingschema.BillingReservationStatusExpired).Error)
		_, err = ReleaseReservation(ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: "expired:" + input.IdempotencyKey})
		require.ErrorIs(t, err, ErrReservationNotOpen)
		require.NoError(t, db.Model(reservation).Update("status", billingschema.BillingReservationStatusOpen).Error)
		_, err = SettleReservation(SettleReservationParams{ReservationID: reservation.ReservationID, ActualAmount: 25, IdempotencyKey: "settle:" + input.IdempotencyKey})
		require.NoError(t, err)
		_, err = ReleaseReservation(ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: "settled:" + input.IdempotencyKey})
		require.ErrorIs(t, err, ErrReservationNotOpen)
		snapshot(t, account, 65, 10)
	})

	t.Run("CancelledSnapshotWaitDoesNotWrite", func(t *testing.T) {
		account := seed(t, 100)
		input := params(account, 25)
		holder := db.Begin()
		require.NoError(t, holder.Error)
		defer holder.Rollback()
		require.NoError(t, holder.Exec("SELECT account_id FROM billing.balance_snapshots WHERE account_id = ? FOR UPDATE", account).Error)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			_, err := CreateReservationTx(tx, input)
			return err
		})
		require.Error(t, err)
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
		require.NoError(t, holder.Rollback().Error)
		snapshot(t, account, 100, 0)
		require.Zero(t, count(t, &billingschema.BillingReservation{}, "account_id", account))
		require.Zero(t, count(t, &billingschema.BillingLedgerEntry{}, "idempotency_key", "entry:"+input.IdempotencyKey))
		require.Zero(t, count(t, &billingschema.BillingOutboxEvent{}, "idempotency_key", "outbox:"+input.IdempotencyKey))
	})

	t.Run("ReleaseLedgerConflictAndUnderflow", func(t *testing.T) {
		account := seed(t, 100)
		input := params(account, 25)
		reservation, err := CreateReservation(input)
		require.NoError(t, err)
		key := "release-conflict:" + input.IdempotencyKey
		require.NoError(t, db.Create(&billingschema.BillingLedgerEntry{AccountID: account, ReferenceID: "different", EntryType: LedgerEntryTypeReserveRelease, IdempotencyKey: key}).Error)
		_, err = ReleaseReservation(ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: key})
		require.ErrorIs(t, err, ErrLedgerConflict)
		snapshot(t, account, 75, 25)
		require.NoError(t, db.Model(&billingschema.BillingBalanceSnapshot{}).Where("account_id = ?", account).Update("reserved_balance", 0).Error)
		_, err = ReleaseReservation(ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: "underflow:" + input.IdempotencyKey})
		require.EqualError(t, err, "reserved balance underflow")
		snapshot(t, account, 75, 0)
	})

	t.Run("MissingSnapshotPreservesInitialization", func(t *testing.T) {
		account := "missing-snapshot:" + platformruntime.GetUUID()
		_, err := CreateReservation(params(account, 1))
		require.ErrorIs(t, err, ErrInsufficientBalance)
		// The newly initialized zero snapshot belongs to the caller transaction,
		// so the insufficient-funds failure must roll it back as before.
		require.Zero(t, count(t, &billingschema.BillingBalanceSnapshot{}, "account_id", account))
	})
}
