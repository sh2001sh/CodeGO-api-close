package domain

import (
	"testing"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestIncreaseReservationIsAtomicAndIdempotent(t *testing.T) {
	platformdb.DB = openLedgerTestDB(t)
	account := seedAccountWithCredit(t, 1101, 1_000)
	reservation, err := CreateReservation(CreateReservationParams{
		AccountID: account.AccountID, RequestID: "req-increase", ReservedAmount: 300, IdempotencyKey: "reserve-increase",
	})
	require.NoError(t, err)

	params := IncreaseReservationParams{
		ReservationID: reservation.ReservationID, Amount: 150, IdempotencyKey: "increase-150",
	}
	first, err := IncreaseReservation(params)
	require.NoError(t, err)
	second, err := IncreaseReservation(params)
	require.NoError(t, err)
	require.Equal(t, first.ReservationID, second.ReservationID)
	require.Equal(t, int64(450), second.ReservedAmount)

	snapshot := loadSnapshot(t, account.AccountID)
	require.Equal(t, int64(550), snapshot.AvailableBalance)
	require.Equal(t, int64(450), snapshot.ReservedBalance)

	var increaseEntries int64
	require.NoError(t, platformdb.DB.Model(&billingschema.BillingLedgerEntry{}).
		Where("idempotency_key = ?", params.IdempotencyKey).Count(&increaseEntries).Error)
	require.Equal(t, int64(1), increaseEntries)
}

func TestIncreaseReservationRejectsInsufficientBalanceWithoutMutation(t *testing.T) {
	platformdb.DB = openLedgerTestDB(t)
	account := seedAccountWithCredit(t, 1102, 400)
	reservation, err := CreateReservation(CreateReservationParams{
		AccountID: account.AccountID, RequestID: "req-increase-insufficient", ReservedAmount: 300, IdempotencyKey: "reserve-insufficient",
	})
	require.NoError(t, err)

	_, err = IncreaseReservation(IncreaseReservationParams{
		ReservationID: reservation.ReservationID, Amount: 150, IdempotencyKey: "increase-insufficient",
	})
	require.ErrorIs(t, err, ErrInsufficientBalance)

	snapshot := loadSnapshot(t, account.AccountID)
	require.Equal(t, int64(100), snapshot.AvailableBalance)
	require.Equal(t, int64(300), snapshot.ReservedBalance)
	var persisted billingschema.BillingReservation
	require.NoError(t, platformdb.DB.Where("reservation_id = ?", reservation.ReservationID).First(&persisted).Error)
	require.Equal(t, int64(300), persisted.ReservedAmount)
}
