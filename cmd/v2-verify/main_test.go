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

func TestVerifyReportsUnmaterializedAccountsWithoutFailing(t *testing.T) {
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
	require.NoError(t, db.Create(&identityschema.User{Id: 1, Username: "verify-user"}).Error)
	require.NoError(t, db.Create(&identityschema.Token{Id: 2, UserId: 1, Key: "verify-token"}).Error)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{Id: 3, UserId: 1}).Error)
	require.NoError(t, db.Create(&commerceschema.BlindBoxCredit{Id: 4, UserId: 1, MigratedAt: 0}).Error)
	require.NoError(t, db.Create(&commerceschema.BlindBoxCredit{Id: 5, UserId: 1, MigratedAt: 1, RemainingAmount: 0, Status: commerceschema.BlindBoxCreditStatusExhausted}).Error)

	report, err := verify(context.Background())
	require.NoError(t, err)
	require.Empty(t, report.MissingMigrations)
	require.Equal(t, 1, report.UnmaterializedWalletAccounts)
	require.Equal(t, 1, report.UnmaterializedClaudeWalletAccounts)
	require.Equal(t, 1, report.UnmaterializedTokenAccounts)
	require.Equal(t, 1, report.UnmaterializedSubscriptionAccounts)
	require.EqualValues(t, 1, report.LegacyBlindBoxCredits)
	require.True(t, report.hasFailures(true))

	report.LegacyBlindBoxCredits = 0
	require.False(t, report.hasFailures(true))
}
