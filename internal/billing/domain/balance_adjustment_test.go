package domain

import (
	"testing"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAdjustAvailableBalanceTxIsAtomicAndIdempotent(t *testing.T) {
	db := openLedgerTestDB(t)
	platformdb.DB = db
	account := seedAccountWithCredit(t, 1101, 1_000)
	params := AdjustAvailableBalanceParams{
		AccountID: account.AccountID, UsageAmount: 250, IdempotencyKey: "adjust-1101",
		ReasonCode: "token_quota_adjustment", ReferenceType: "token", ReferenceID: "1101",
	}

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := AdjustAvailableBalanceTx(tx, params)
		return err
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := AdjustAvailableBalanceTx(tx, params)
		return err
	}))

	snapshot := loadSnapshot(t, account.AccountID)
	require.EqualValues(t, 750, snapshot.AvailableBalance)
	require.EqualValues(t, 250, snapshot.ConsumedTotal)

	var entries int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", params.IdempotencyKey).Count(&entries).Error)
	require.EqualValues(t, 1, entries)
}

func TestAdjustAvailableBalanceTxRejectsOppositeIdempotentDirection(t *testing.T) {
	db := openLedgerTestDB(t)
	platformdb.DB = db
	account := seedAccountWithCredit(t, 1102, 1_000)
	consume := AdjustAvailableBalanceParams{AccountID: account.AccountID, UsageAmount: 100, IdempotencyKey: "adjust-1102"}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := AdjustAvailableBalanceTx(tx, consume)
		return err
	}))

	refund := consume
	refund.UsageAmount = -100
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := AdjustAvailableBalanceTx(tx, refund)
		return err
	})
	require.ErrorIs(t, err, ErrLedgerConflict)
}
