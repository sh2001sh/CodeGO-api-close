package app

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRecoverStaleRelayReservationsReleasesRequestWithoutUsage(t *testing.T) {
	db := setupStaleRecoveryTestDB(t)
	now := time.Now().UTC()
	account := seedStaleRecoveryReservation(t, db, 5001, "stale-no-usage", 300, now.Add(-time.Hour))

	result, err := RecoverStaleRelayReservations(context.Background(), now, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Requests)
	require.Equal(t, 1, result.Released)
	require.Zero(t, result.Settled)

	assertRecoveredBalance(t, db, account.AccountID, 1_000, 0, 0)
	var user identityschema.User
	require.NoError(t, db.Where("id = ?", 5001).First(&user).Error)
	require.Equal(t, 1_000, user.ClaudeQuota)
}

func TestRecoverStaleRelayReservationsSettlesFromUsageLog(t *testing.T) {
	db := setupStaleRecoveryTestDB(t)
	now := time.Now().UTC()
	account := seedStaleRecoveryReservation(t, db, 5002, "stale-with-usage", 300, now.Add(-time.Hour))
	require.NoError(t, db.Create(&auditschema.Log{
		UserId: 5002, Type: auditschema.LogTypeConsume, RequestId: "stale-with-usage", Quota: 500,
	}).Error)

	result, err := RecoverStaleRelayReservations(context.Background(), now, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Requests)
	require.Equal(t, 1, result.Settled)
	require.Zero(t, result.Released)
	require.Zero(t, result.Capped)

	assertRecoveredBalance(t, db, account.AccountID, 500, 0, 500)
	var user identityschema.User
	require.NoError(t, db.Where("id = ?", 5002).First(&user).Error)
	require.Equal(t, 500, user.ClaudeQuota)
}

func TestRecoverStaleRelayReservationsWaitsUntilExpiry(t *testing.T) {
	db := setupStaleRecoveryTestDB(t)
	now := time.Now().UTC()
	account := seedStaleRecoveryReservation(t, db, 5003, "not-expired", 300, now.Add(time.Hour))

	result, err := RecoverStaleRelayReservations(context.Background(), now, 10)
	require.NoError(t, err)
	require.Zero(t, result.Requests)
	assertRecoveredBalance(t, db, account.AccountID, 700, 300, 0)

	result, err = RecoverStaleRelayReservations(context.Background(), now.Add(2*time.Hour), 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Requests)
	require.Equal(t, 1, result.Released)
	assertRecoveredBalance(t, db, account.AccountID, 1_000, 0, 0)
}

func TestRecoverStaleRelayReservationsCapsUsageAtAccountCapacity(t *testing.T) {
	db := setupStaleRecoveryTestDB(t)
	now := time.Now().UTC()
	account := seedStaleRecoveryReservation(t, db, 5004, "stale-capped", 300, now.Add(-time.Hour))
	require.NoError(t, db.Create(&auditschema.Log{
		UserId: 5004, Type: auditschema.LogTypeConsume, RequestId: "stale-capped", Quota: 1_500,
	}).Error)

	result, err := RecoverStaleRelayReservations(context.Background(), now, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Requests)
	require.Equal(t, 1, result.Settled)
	require.Equal(t, 1, result.Capped)
	require.EqualValues(t, 1_000, result.Amount)
	assertRecoveredBalance(t, db, account.AccountID, 0, 0, 1_000)
}

func TestRecoverStaleRelayReservationsUsesRequestEconomicsBeforeLog(t *testing.T) {
	db := setupStaleRecoveryTestDB(t)
	now := time.Now().UTC()
	account := seedStaleRecoveryReservation(t, db, 5005, "stale-economics", 300, now.Add(-time.Hour))
	require.NoError(t, db.Create(&billingschema.RequestEconomics{
		RequestID: "stale-economics", ActualAmount: 400,
	}).Error)
	require.NoError(t, db.Create(&auditschema.Log{
		UserId: 5005, Type: auditschema.LogTypeConsume, RequestId: "stale-economics", Quota: 700,
	}).Error)

	result, err := RecoverStaleRelayReservations(context.Background(), now, 10)
	require.NoError(t, err)
	require.EqualValues(t, 400, result.Amount)
	assertRecoveredBalance(t, db, account.AccountID, 600, 0, 400)
}

func TestRecoverStaleRelayReservationsDistributesUsageAcrossReservations(t *testing.T) {
	db := setupStaleRecoveryTestDB(t)
	now := time.Now().UTC()
	account := seedStaleRecoveryReservation(t, db, 5006, "stale-multiple", 200, now.Add(-time.Hour))
	_, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{
		AccountID: account.AccountID, RequestID: "stale-multiple", ReservedAmount: 300,
		IdempotencyKey: "stale-multiple:reserve-extra", ExpiresAt: expiredAt(now),
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&auditschema.Log{
		UserId: 5006, Type: auditschema.LogTypeConsume, RequestId: "stale-multiple", Quota: 450,
	}).Error)

	result, err := RecoverStaleRelayReservations(context.Background(), now, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Requests)
	require.Equal(t, 2, result.Settled)
	require.EqualValues(t, 450, result.Amount)
	assertRecoveredBalance(t, db, account.AccountID, 550, 0, 450)

	var settlementCount int64
	require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Count(&settlementCount).Error)
	require.EqualValues(t, 2, settlementCount)
	var settledTotal int64
	require.NoError(t, db.Model(&billingschema.BillingSettlement{}).
		Select("COALESCE(SUM(actual_amount), 0)").Scan(&settledTotal).Error)
	require.EqualValues(t, 450, settledTotal)
}

func TestRecoverStaleSubscriptionCorrectsHistoricalRetryOvercount(t *testing.T) {
	db := setupStaleRecoveryTestDB(t)
	now := time.Now().UTC()
	account := seedStaleSubscriptionReservations(t, db, 5101, 6101, "stale-subscription", now)
	require.NoError(t, db.Create(&auditschema.Log{
		UserId: 5101, Type: auditschema.LogTypeConsume, RequestId: "stale-subscription", Quota: 900,
		Other: `{"subscription_consumed":900,"subscription_pre_consumed":700}`,
	}).Error)

	result, err := RecoverStaleRelayReservations(context.Background(), now, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Requests)
	require.Equal(t, 2, result.Settled)
	require.EqualValues(t, 700, result.Amount)
	assertRecoveredBalance(t, db, account.AccountID, 300, 0, 700)

	var subscription commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", 6101).First(&subscription).Error)
	require.EqualValues(t, 700, subscription.AmountUsed)
}

func TestRecoverStaleSubscriptionFallsBackToQuotaWhenJSONFieldIsMissing(t *testing.T) {
	db := setupStaleRecoveryTestDB(t)
	now := time.Now().UTC()
	account := seedStaleSubscriptionReservations(t, db, 5102, 6102, "stale-subscription-no-json", now)
	require.NoError(t, db.Create(&auditschema.Log{
		UserId: 5102, Type: auditschema.LogTypeConsume, RequestId: "stale-subscription-no-json", Quota: 400, Other: `{}`,
	}).Error)

	result, err := RecoverStaleRelayReservations(context.Background(), now, 10)
	require.NoError(t, err)
	require.EqualValues(t, 400, result.Amount)
	assertRecoveredBalance(t, db, account.AccountID, 600, 0, 400)
}

func TestRecoverStaleSubscriptionSettlesWhenProjectionWasDeleted(t *testing.T) {
	db := setupStaleRecoveryTestDB(t)
	now := time.Now().UTC()
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: 6201, QuotaUnit: billingQuotaUnitQuota,
	})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: 1_000, IdempotencyKey: "deleted-subscription:credit",
	})
	require.NoError(t, err)
	_, err = billingdomain.CreateReservation(billingdomain.CreateReservationParams{
		AccountID: account.AccountID, RequestID: "deleted-subscription", ReservedAmount: 300,
		IdempotencyKey: "deleted-subscription:reserve", ExpiresAt: expiredAt(now),
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&auditschema.Log{
		Type: auditschema.LogTypeConsume, RequestId: "deleted-subscription", Quota: 400,
	}).Error)

	result, err := RecoverStaleRelayReservations(context.Background(), now, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Requests)
	require.Equal(t, 1, result.Settled)
	require.EqualValues(t, 400, result.Amount)
	assertRecoveredBalance(t, db, account.AccountID, 600, 0, 400)
}

func setupStaleRecoveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false
	require.NoError(t, db.AutoMigrate(
		&identityschema.User{}, &auditschema.Log{}, &billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{}, &billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{}, &billingschema.RequestEconomics{},
		&commerceschema.UserSubscription{}, &commerceschema.SubscriptionPreConsumeRecord{},
	))
	return db
}

func seedStaleSubscriptionReservations(t *testing.T, db *gorm.DB, userID, subscriptionID int, requestID string, now time.Time) billingschema.BillingAccount {
	t.Helper()
	require.NoError(t, db.Create(&identityschema.User{Id: userID, Username: requestID}).Error)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{
		Id: subscriptionID, UserId: userID, AmountTotal: 1_000, AmountUsed: 500, Status: "active",
	}).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(subscriptionID), QuotaUnit: billingQuotaUnitQuota,
	})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: 1_000, IdempotencyKey: requestID + ":credit",
	})
	require.NoError(t, err)
	for index, amount := range []int64{300, 200} {
		_, err = billingdomain.CreateReservation(billingdomain.CreateReservationParams{
			AccountID: account.AccountID, RequestID: requestID, ReservedAmount: amount,
			IdempotencyKey: requestID + ":reserve:" + string(rune('0'+index)), ExpiresAt: expiredAt(now),
		})
		require.NoError(t, err)
	}
	return *account
}

func expiredAt(now time.Time) *time.Time {
	expiresAt := now.Add(-time.Hour)
	return &expiresAt
}

func seedStaleRecoveryReservation(t *testing.T, db *gorm.DB, userID int, requestID string, amount int64, expiresAt time.Time) billingschema.BillingAccount {
	t.Helper()
	require.NoError(t, db.Create(&identityschema.User{Id: userID, Username: requestID, ClaudeQuota: 1_000 - int(amount)}).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: billingAccountTypeClaudeWallet, OwnerType: billingOwnerTypeUser, OwnerID: int64(userID), QuotaUnit: billingQuotaUnitQuota,
	})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1_000, IdempotencyKey: requestID + ":credit"})
	require.NoError(t, err)
	_, err = billingdomain.CreateReservation(billingdomain.CreateReservationParams{
		AccountID: account.AccountID, RequestID: requestID, ReservedAmount: amount,
		IdempotencyKey: requestID + ":reserve", ExpiresAt: &expiresAt,
	})
	require.NoError(t, err)
	return *account
}

func assertRecoveredBalance(t *testing.T, db *gorm.DB, accountID string, available, reserved, consumed int64) {
	t.Helper()
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", accountID).First(&snapshot).Error)
	require.Equal(t, available, snapshot.AvailableBalance)
	require.Equal(t, reserved, snapshot.ReservedBalance)
	require.Equal(t, consumed, snapshot.ConsumedTotal)
}
