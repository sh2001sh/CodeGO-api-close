package app

import (
	"testing"

	"github.com/glebarez/sqlite"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAdjustTokenQuotaUsesLedgerAndProjectsLegacyFields(t *testing.T) {
	db, token := openTokenQuotaTestDB(t)

	require.NoError(t, AdjustTokenQuota(token.Id, token.Key, 250))
	loaded, err := GetTokenByID(token.Id)
	require.NoError(t, err)
	require.Equal(t, 750, loaded.RemainQuota)

	var legacy identityschema.Token
	require.NoError(t, db.First(&legacy, token.Id).Error)
	require.Equal(t, 750, legacy.RemainQuota)
	require.Equal(t, 250, legacy.UsedQuota)

	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "token", token.Id, "token").First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	require.EqualValues(t, 750, snapshot.AvailableBalance)
	require.EqualValues(t, 250, snapshot.ConsumedTotal)

	var reservations int64
	var settlements int64
	require.NoError(t, db.Model(&billingschema.BillingReservation{}).Count(&reservations).Error)
	require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Count(&settlements).Error)
	require.Zero(t, reservations)
	require.Zero(t, settlements)
}

func TestAdjustTokenQuotaInsufficientBalanceRollsBackLedgerAndLegacy(t *testing.T) {
	db, token := openTokenQuotaTestDB(t)

	err := AdjustTokenQuota(token.Id, token.Key, 1_001)
	require.ErrorIs(t, err, identitystore.ErrTokenQuotaInsufficient)

	var legacy identityschema.Token
	require.NoError(t, db.First(&legacy, token.Id).Error)
	require.Equal(t, 1_000, legacy.RemainQuota)
	require.Zero(t, legacy.UsedQuota)

	var entries int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&entries).Error)
	require.Zero(t, entries)
}

func TestAdjustTokenQuotaRefundUpdatesBothProjections(t *testing.T) {
	db, token := openTokenQuotaTestDB(t)
	require.NoError(t, AdjustTokenQuota(token.Id, token.Key, 250))
	require.NoError(t, AdjustTokenQuota(token.Id, token.Key, -100))

	var legacy identityschema.Token
	require.NoError(t, db.First(&legacy, token.Id).Error)
	require.Equal(t, 850, legacy.RemainQuota)
	require.Equal(t, 150, legacy.UsedQuota)

	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "token", token.Id, "token").First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	require.EqualValues(t, 850, snapshot.AvailableBalance)
	require.EqualValues(t, 250, snapshot.ConsumedTotal)
	require.EqualValues(t, 100, snapshot.RefundedTotal)
}

func TestAdjustTokenQuotaReconcilesPreexistingProjectionDrift(t *testing.T) {
	db, token := openTokenQuotaTestDB(t)
	require.NoError(t, AdjustTokenQuota(token.Id, token.Key, 250))
	require.NoError(t, db.Model(&identityschema.Token{}).Where("id = ?", token.Id).Update("remain_quota", 900).Error)

	require.NoError(t, AdjustTokenQuota(token.Id, token.Key, 100))

	var legacy identityschema.Token
	require.NoError(t, db.First(&legacy, token.Id).Error)
	require.Equal(t, 800, legacy.RemainQuota)

	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "token", token.Id, "token").First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	require.EqualValues(t, 800, snapshot.AvailableBalance)
	require.EqualValues(t, 350, snapshot.ConsumedTotal)
	require.EqualValues(t, 150, snapshot.RefundedTotal)
}

func openTokenQuotaTestDB(t *testing.T) (*gorm.DB, *identityschema.Token) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	originalRedisEnabled := platformcache.RedisEnabled
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
		platformcache.RedisEnabled = originalRedisEnabled
	})
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false
	platformcache.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(
		&identityschema.Token{},
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{},
	))
	token := &identityschema.Token{Id: 901, UserId: 7, Key: "ledger-token", RemainQuota: 1_000}
	require.NoError(t, db.Create(token).Error)
	return db, token
}
