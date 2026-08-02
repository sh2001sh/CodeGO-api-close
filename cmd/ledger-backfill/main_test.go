package main

import (
	"testing"

	"github.com/glebarez/sqlite"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestApplyBackfillCreatesLedgerAccountsIdempotently(t *testing.T) {
	db := setupLedgerBackfillTestDB(t)
	require.NoError(t, db.Create(&identityschema.User{Id: 101, Username: "backfill-user", AffCode: "backfill-101", Quota: 1_000, ClaudeQuota: 250}).Error)
	require.NoError(t, db.Create(&identityschema.Token{Id: 301, UserId: 101, Key: "backfill-token", RemainQuota: 125}).Error)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{Id: 201, UserId: 101, AmountTotal: 900, AmountUsed: 300, Status: "active"}).Error)
	require.NoError(t, db.Create(&identityschema.User{Id: 102, Username: "negative-balance-user", AffCode: "backfill-102", Quota: -100, ClaudeQuota: -25}).Error)
	require.NoError(t, db.Create(&identityschema.Token{Id: 302, UserId: 102, Key: "negative-balance-token", RemainQuota: -50}).Error)

	require.NoError(t, applyBackfill(0))
	require.NoError(t, applyBackfill(0))

	var accounts []billingschema.BillingAccount
	require.NoError(t, db.Order("account_type asc").Find(&accounts).Error)
	require.Len(t, accounts, 7)
	var entries []billingschema.BillingLedgerEntry
	require.NoError(t, db.Find(&entries).Error)
	require.Len(t, entries, 4)
}

func TestNegativeWalletLegacyNormalizationIsAuditableAndIdempotent(t *testing.T) {
	db := setupLedgerBackfillTestDB(t)
	createNegativeWalletFixture(t, db, 101, "wallet-normalize", -500)
	createNegativeWalletFixture(t, db, 102, "wallet-entry", -300)
	createNegativeWalletFixture(t, db, 103, "wallet-balance", -200)
	createNegativeWalletFixture(t, db, 104, "wallet-reserved", -100)

	require.NoError(t, db.Create(&billingschema.BillingLedgerEntry{AccountID: "wallet-entry", EntryID: "existing-entry", IdempotencyKey: "existing-entry-key"}).Error)
	require.NoError(t, db.Model(&billingschema.BillingBalanceSnapshot{}).Where("account_id = ?", "wallet-balance").Update("available_balance", 1).Error)
	require.NoError(t, db.Create(&billingschema.BillingReservation{AccountID: "wallet-reserved", ReservationID: "open-reservation", IdempotencyKey: "open-reservation-key", Status: billingschema.BillingReservationStatusOpen, ReservedAmount: 1}).Error)

	plan, err := inspectNegativeWalletLegacy(0)
	require.NoError(t, err)
	require.Len(t, plan.Candidates, 1)
	require.Equal(t, 101, plan.Candidates[0].UserID)
	require.EqualValues(t, 500, plan.TotalNormalizedQuota)

	require.NoError(t, applyNegativeWalletLegacyNormalization(0))
	require.NoError(t, applyNegativeWalletLegacyNormalization(0))

	var normalized identityschema.User
	require.NoError(t, db.First(&normalized, 101).Error)
	require.Zero(t, normalized.Quota)
	var entry billingschema.BillingLedgerEntry
	require.NoError(t, db.Where("account_id = ?", "wallet-normalize").First(&entry).Error)
	require.Equal(t, "legacy_negative_quota_normalized", entry.ReasonCode)
	require.EqualValues(t, 0, entry.Amount)
	require.Equal(t, "migration", entry.OperatorType)
	require.Contains(t, entry.ReasonDetail, "-500")
	require.JSONEq(t, `{"migration":"legacy_negative_wallet_normalization","previous_legacy_quota":-500,"canonical_balance":0}`, string(entry.Metadata))
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&snapshot, "account_id = ?", "wallet-normalize").Error)
	require.Zero(t, snapshot.AvailableBalance)
	require.Zero(t, snapshot.ReservedBalance)
	require.Zero(t, snapshot.GrantedTotal)
	require.Zero(t, snapshot.ConsumedTotal)
	require.Zero(t, snapshot.RefundedTotal)
	var entryCount int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ?", "wallet-normalize").Count(&entryCount).Error)
	require.EqualValues(t, 1, entryCount)

	for _, userID := range []int{102, 103, 104} {
		var user identityschema.User
		require.NoError(t, db.First(&user, userID).Error)
		require.Negative(t, user.Quota)
	}
}

func setupLedgerBackfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &identityschema.Token{}, &commerceschema.UserSubscription{}, &commerceschema.BlindBoxCredit{}, &commerceschema.BlindBoxOpenRecord{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingOutboxEvent{}, &billingschema.BillingReservation{}))
	return db
}

func createNegativeWalletFixture(t *testing.T, db *gorm.DB, userID int, accountID string, quota int) {
	t.Helper()
	require.NoError(t, db.Create(&identityschema.User{Id: userID, Username: accountID, AffCode: accountID, Quota: quota}).Error)
	require.NoError(t, db.Create(&billingschema.BillingAccount{AccountID: accountID, AccountType: "wallet", OwnerType: "user", OwnerID: int64(userID), QuotaUnit: "quota"}).Error)
	require.NoError(t, db.Create(&billingschema.BillingBalanceSnapshot{AccountID: accountID}).Error)
}
