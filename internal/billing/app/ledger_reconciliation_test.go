package app

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCountLedgerInconsistenciesUsesOneConsistentAggregate(t *testing.T) {
	originalDB := platformdb.DB
	originalUsingSQLite := platformdb.UsingSQLite
	originalUsingPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalUsingSQLite
		platformdb.UsingPostgreSQL = originalUsingPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false
	require.NoError(t, db.AutoMigrate(
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
	))

	account := billingschema.BillingAccount{AccountID: "consistent-count", OwnerType: "user", OwnerID: 1, AccountType: "wallet", QuotaUnit: "quota"}
	require.NoError(t, db.Create(&account).Error)
	require.NoError(t, db.Create(&billingschema.BillingBalanceSnapshot{AccountID: account.AccountID, AvailableBalance: 1_000, GrantedTotal: 1_000}).Error)
	require.NoError(t, db.Create(&billingschema.BillingLedgerEntry{AccountID: account.AccountID, EntryType: "grant_credit", Amount: 1_000, IdempotencyKey: "grant"}).Error)

	count, err := CountLedgerInconsistencies(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)

	require.NoError(t, db.Model(&billingschema.BillingBalanceSnapshot{}).Where("account_id = ?", account.AccountID).Update("available_balance", 999).Error)
	count, err = CountLedgerInconsistencies(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
