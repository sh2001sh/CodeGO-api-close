package domain

import (
	"math"
	"testing"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestCreditAccountRejectsIntegerOverflowWithoutWrites(t *testing.T) {
	for _, consumed := range []bool{false, true} {
		name := "available"
		if consumed {
			name = "lifetime_granted"
		}
		t.Run(name, func(t *testing.T) {
			db := openLedgerTestDB(t)
			previousDB := platformdb.DB
			platformdb.DB = db
			t.Cleanup(func() { platformdb.DB = previousDB })
			account := seedAccountWithCredit(t, 16001, math.MaxInt64)
			if consumed {
				reservation, err := CreateReservation(CreateReservationParams{AccountID: account.AccountID, RequestID: name, ReservedAmount: math.MaxInt64, IdempotencyKey: "overflow:reserve"})
				require.NoError(t, err)
				_, err = SettleReservation(SettleReservationParams{ReservationID: reservation.ReservationID, ActualAmount: math.MaxInt64, IdempotencyKey: "overflow:settle"})
				require.NoError(t, err)
			}
			before := loadSnapshot(t, account.AccountID)
			_, err := CreditAccount(CreditAccountParams{AccountID: account.AccountID, Amount: 1, IdempotencyKey: "overflow:credit"})
			require.ErrorContains(t, err, "credit exceeds integer limit")
			require.Equal(t, before, loadSnapshot(t, account.AccountID))
			var entries, events int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", "overflow:credit").Count(&entries).Error)
			require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Where("idempotency_key = ?", "outbox:overflow:credit").Count(&events).Error)
			require.Zero(t, entries)
			require.Zero(t, events)
		})
	}
}

func TestCreditAccountExactIntegerLimitAllowsIdempotentReplay(t *testing.T) {
	db := openLedgerTestDB(t)
	previousDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = previousDB })
	account := seedAccountWithCredit(t, 16002, math.MaxInt64-1)
	params := CreditAccountParams{AccountID: account.AccountID, Amount: 1, IdempotencyKey: "limit:credit"}
	first, err := CreditAccount(params)
	require.NoError(t, err)
	replayed, err := CreditAccount(params)
	require.NoError(t, err)
	require.Equal(t, first.EntryID, replayed.EntryID)
	snapshot := loadSnapshot(t, account.AccountID)
	require.EqualValues(t, math.MaxInt64, snapshot.AvailableBalance)
	require.EqualValues(t, math.MaxInt64, snapshot.GrantedTotal)
	params.IdempotencyKey = "limit:overflow"
	_, err = CreditAccount(params)
	require.ErrorContains(t, err, "credit exceeds integer limit")
	require.Equal(t, snapshot, loadSnapshot(t, account.AccountID))
}
