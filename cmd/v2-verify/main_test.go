package main

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestVerifyReportsMissingBackfillAccounts(t *testing.T) {
	db := verifyTestDatabase(t)
	require.NoError(t, db.Create(&identityschema.User{Id: 1, Username: "verify-user", Quota: 10, ClaudeQuota: 20}).Error)
	require.NoError(t, db.Create(&identityschema.Token{Id: 2, UserId: 1, Key: "verify-token", RemainQuota: 30}).Error)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{Id: 3, UserId: 1, AmountTotal: 40}).Error)
	require.NoError(t, db.Create(&commerceschema.BlindBoxCredit{Id: 4, UserId: 1, MigratedAt: 0}).Error)
	require.NoError(t, db.Create(&commerceschema.BlindBoxCredit{Id: 5, UserId: 1, MigratedAt: 1, RemainingAmount: 0, Status: commerceschema.BlindBoxCreditStatusExhausted}).Error)

	report, err := verify(context.Background())
	require.NoError(t, err)
	require.Empty(t, report.MissingMigrations)
	require.Equal(t, 1, report.MissingWalletAccounts)
	require.Equal(t, 1, report.MissingClaudeAccounts)
	require.Equal(t, 1, report.MissingTokenAccounts)
	require.Equal(t, 1, report.MissingSubscriptionFunds)
	require.EqualValues(t, 1, report.LegacyBlindBoxCredits)
	require.True(t, report.hasFailures(true))
}

func TestUserAccountRequiresVerificationForHistoricalUsage(t *testing.T) {
	user := &identityschema.User{UsedQuota: 1}
	require.True(t, userAccountRequiresVerification(user, "wallet"))
	require.False(t, userAccountRequiresVerification(user, "claude_wallet"))
}

func TestVerifyAllowsUnfundedAccountsToBeCreatedLazily(t *testing.T) {
	db := verifyTestDatabase(t)
	require.NoError(t, db.Create(&identityschema.User{Id: 1, Username: "lazy-user"}).Error)
	require.NoError(t, db.Create(&identityschema.Token{Id: 2, UserId: 1, Key: "lazy-token"}).Error)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{Id: 3, UserId: 1}).Error)

	report, err := verify(context.Background())
	require.NoError(t, err)
	require.Zero(t, report.MissingWalletAccounts)
	require.Zero(t, report.MissingClaudeAccounts)
	require.Zero(t, report.MissingTokenAccounts)
	require.Zero(t, report.MissingSubscriptionFunds)
}

func verifyTestDatabase(t *testing.T) *gorm.DB {
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

	require.NoError(t, db.AutoMigrate(
		&identityschema.User{},
		&identityschema.Token{},
		&commerceschema.UserSubscription{},
		&commerceschema.BlindBoxCredit{},
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{},
	))
	require.NoError(t, db.Exec("CREATE TABLE platform_schema_migrations (id varchar(128) PRIMARY KEY, applied_at datetime)").Error)
	for _, id := range platformstore.V2MigrationIDs() {
		require.NoError(t, db.Exec("INSERT INTO platform_schema_migrations (id) VALUES (?)", id).Error)
	}
	return db
}
