package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type subscriptionPreconsumeSQLCounter struct {
	logger.Interface
	locked                             bool
	statements, accounts, ledgerChecks int
}

func (c *subscriptionPreconsumeSQLCounter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, _ := fc()
	sql = strings.ToLower(strings.NewReplacer("`", "", "\"", "").Replace(sql))
	if c.locked {
		c.statements++
		if strings.HasPrefix(sql, "select") {
			if strings.Contains(sql, "from billing_accounts") || strings.Contains(sql, "from billing.accounts") {
				c.accounts++
			}
			if strings.Contains(sql, "from billing_ledger_entries") || strings.Contains(sql, "from billing.ledger_entries") {
				c.ledgerChecks++
			}
		}
	} else if err == nil && strings.Contains(sql, "from user_subscriptions") {
		c.locked = true
	}
	c.Interface.Trace(ctx, begin, fc, err)
}

func insertPreconsumeReuseSubscription(t *testing.T, db *gorm.DB) (*commerceschema.SubscriptionPlan, *commerceschema.UserSubscription) {
	t.Helper()
	plan := &commerceschema.SubscriptionPlan{Title: "Preconsume reuse fixture", TotalAmount: 1_000, QuotaResetPeriod: commerceschema.SubscriptionResetNever}
	require.NoError(t, db.Create(plan).Error)
	now := time.Now().Unix()
	sub := &commerceschema.UserSubscription{UserId: plan.Id, PlanId: plan.Id, AmountTotal: 1_000, StartTime: now - 3600, EndTime: now + 3600, Status: "active"}
	require.NoError(t, db.Create(sub).Error)
	return plan, sub
}

func seedPreconsumeReuseAccount(t *testing.T, sub *commerceschema.UserSubscription, credit bool) *billingschema.BillingAccount {
	t.Helper()
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota"})
	require.NoError(t, err)
	if credit {
		_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1_000, IdempotencyKey: "reuse-seed:" + account.AccountID, ReasonCode: "test"})
		require.NoError(t, err)
	}
	return account
}

func checkPreconsumeReuseHotPath(t *testing.T, db *gorm.DB, statements int) {
	t.Helper()
	plan, sub := insertPreconsumeReuseSubscription(t, db)
	account := seedPreconsumeReuseAccount(t, sub, true)
	counter := &subscriptionPreconsumeSQLCounter{Interface: logger.Default.LogMode(logger.Silent)}
	platformdb.DB = db.Session(&gorm.Session{Logger: counter})
	requestID := "reuse:" + account.AccountID
	result, available, err := preConsumeSubscriptionCandidate(requestID, sub.UserId, "", 100, time.Now().Unix(), sub.Id, plan)
	require.NoError(t, err)
	require.True(t, available)
	assert.Equal(t, statements, counter.statements, "SQL after acquiring the subscription row lock, excluding lock SELECT and BEGIN/COMMIT")
	assert.Equal(t, 1, counter.accounts, "the already-read account must not be queried again")
	assert.Equal(t, 1, counter.ledgerChecks, "ledger-backed state must not be checked twice")
	t.Logf("locked preconsume SQL=%d; account SELECT=%d; ledger existence SELECT=%d", counter.statements, counter.accounts, counter.ledgerChecks)
	platformdb.DB = db
	duplicate, available, err := preConsumeSubscriptionCandidate(requestID, sub.UserId, "", 100, time.Now().Unix(), sub.Id, plan)
	require.NoError(t, err)
	require.True(t, available)
	assert.Equal(t, result.PreConsumed, duplicate.PreConsumed)
	var reloaded commerceschema.UserSubscription
	require.NoError(t, db.First(&reloaded, sub.Id).Error)
	assert.EqualValues(t, 100, reloaded.AmountUsed)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	assert.EqualValues(t, 900, snapshot.AvailableBalance)
	assert.EqualValues(t, 100, snapshot.ReservedBalance)
	for _, item := range []struct {
		model    any
		expected int64
	}{
		{&billingschema.BillingLedgerEntry{}, 2}, {&billingschema.BillingOutboxEvent{}, 2}, {&billingschema.BillingReservation{}, 1},
	} {
		var count int64
		require.NoError(t, db.Model(item.model).Where("account_id = ?", account.AccountID).Count(&count).Error)
		assert.Equal(t, item.expected, count)
	}
}

func TestSubscriptionPreConsumeTransactionReusesLockedAccount(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)
	checkPreconsumeReuseHotPath(t, db, 13) // SQLite reservation path is four statements longer than PostgreSQL.
}

func TestSubscriptionPreConsumeTransactionReusesLockedAccountPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_LEDGER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("real PostgreSQL fixture not configured: set TEST_LEDGER_POSTGRES_DSN")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	oldDB, oldPG, oldSQLite := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = db, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = oldDB, oldPG, oldSQLite
		_ = sqlDB.Close()
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&commerceschema.SubscriptionPlan{}, &commerceschema.UserSubscription{}, &commerceschema.SubscriptionPreConsumeRecord{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingOutboxEvent{}))
	checkPreconsumeReuseHotPath(t, db, 9)
}

func TestSubscriptionPreConsumeTransactionReusePreservesInitialization(t *testing.T) {
	for _, state := range []string{"no_account", "empty_account", "missing_snapshot"} {
		t.Run(state, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			ensureSubscriptionPreConsumeRecordSchema(t)
			plan, sub := insertPreconsumeReuseSubscription(t, db)
			if state != "no_account" {
				account := seedPreconsumeReuseAccount(t, sub, state == "missing_snapshot")
				if state == "missing_snapshot" {
					require.NoError(t, db.Where("account_id = ?", account.AccountID).Delete(&billingschema.BillingBalanceSnapshot{}).Error)
				}
			}
			result, available, err := preConsumeSubscriptionCandidate("reuse-init", sub.UserId, "", 100, time.Now().Unix(), sub.Id, plan)
			if state == "missing_snapshot" {
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
				var records int64
				require.NoError(t, db.Model(&commerceschema.SubscriptionPreConsumeRecord{}).Count(&records).Error)
				assert.Zero(t, records)
				return
			}
			require.NoError(t, err)
			require.True(t, available)
			var snapshot billingschema.BillingBalanceSnapshot
			require.NoError(t, db.Where("account_id = ?", result.AccountID).First(&snapshot).Error)
			assert.EqualValues(t, 900, snapshot.AvailableBalance)
			assert.EqualValues(t, 100, snapshot.ReservedBalance)
			var bootstrap int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ? AND reason_code = ?", result.AccountID, "subscription_balance_bootstrap").Count(&bootstrap).Error)
			assert.EqualValues(t, 1, bootstrap)
		})
	}
}

func TestSubscriptionPreConsumeTransactionReuseRollsBackInsufficientQuota(t *testing.T) {
	for _, limit := range []string{"canonical", "period", "reservation"} {
		t.Run(limit, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			ensureSubscriptionPreConsumeRecordSchema(t)
			plan, sub := insertPreconsumeReuseSubscription(t, db)
			account := seedPreconsumeReuseAccount(t, sub, true)
			if limit == "period" {
				plan.PeriodAmount = 50
				sub.PeriodAmount = 50
				require.NoError(t, db.Save(plan).Error)
			}
			sub.AmountUsed = 30 // A failed candidate must roll back even the projection correction made during the canonical check.
			require.NoError(t, db.Save(sub).Error)
			amount := int64(100)
			if limit == "canonical" {
				amount = 1_001
			}
			if limit == "reservation" {
				require.NoError(t, db.Callback().Create().After("gorm:create").Register("test:exhaust_before_reserve", func(tx *gorm.DB) {
					if tx.Statement.Table == "subscription_pre_consume_records" {
						tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Model(&billingschema.BillingBalanceSnapshot{}).
							Where("account_id = ?", account.AccountID).Update("available_balance", 0).Error)
					}
				}))
				t.Cleanup(func() { _ = db.Callback().Create().Remove("test:exhaust_before_reserve") })
			}
			_, available, err := preConsumeSubscriptionCandidate("reuse-insufficient", sub.UserId, "", amount, time.Now().Unix(), sub.Id, plan)
			if limit == "reservation" {
				require.ErrorIs(t, err, billingdomain.ErrInsufficientBalance)
			} else {
				require.NoError(t, err)
			}
			assert.False(t, available)
			var reloaded commerceschema.UserSubscription
			require.NoError(t, db.First(&reloaded, sub.Id).Error)
			assert.EqualValues(t, 30, reloaded.AmountUsed)
			var snapshot billingschema.BillingBalanceSnapshot
			require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
			assert.EqualValues(t, 1_000, snapshot.AvailableBalance)
			assert.Zero(t, snapshot.ReservedBalance)
			for _, item := range []struct {
				model    any
				expected int64
			}{
				{&billingschema.BillingLedgerEntry{}, 1}, {&billingschema.BillingOutboxEvent{}, 1}, {&billingschema.BillingReservation{}, 0},
			} {
				var count int64
				require.NoError(t, db.Model(item.model).Where("account_id = ?", account.AccountID).Count(&count).Error)
				assert.Equal(t, item.expected, count)
			}
			var records int64
			require.NoError(t, db.Model(&commerceschema.SubscriptionPreConsumeRecord{}).Count(&records).Error)
			assert.Zero(t, records)
		})
	}
}

func TestSubscriptionPreConsumeTransactionReusePreservesPeriodicReset(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)
	plan, sub := insertPreconsumeReuseSubscription(t, db)
	account := seedPreconsumeReuseAccount(t, sub, true)
	_, err := billingdomain.AdjustAvailableBalanceTx(db, billingdomain.AdjustAvailableBalanceParams{
		AccountID: account.AccountID, UsageAmount: 900, IdempotencyKey: "reuse-before-reset", ReasonCode: "test",
	})
	require.NoError(t, err)
	plan.QuotaResetPeriod = commerceschema.SubscriptionResetDaily
	require.NoError(t, db.Save(plan).Error)
	sub.AmountUsed, sub.StartTime = 900, time.Now().Add(-48*time.Hour).Unix()
	require.NoError(t, db.Save(sub).Error)
	_, available, err := preConsumeSubscriptionCandidate("reuse-reset", sub.UserId, "", 100, time.Now().Unix(), sub.Id, plan)
	require.NoError(t, err)
	require.True(t, available)
	var reloaded commerceschema.UserSubscription
	require.NoError(t, db.First(&reloaded, sub.Id).Error)
	assert.EqualValues(t, 100, reloaded.AmountUsed)
	assert.Greater(t, reloaded.LastResetTime, sub.StartTime)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	assert.EqualValues(t, 900, snapshot.AvailableBalance)
	assert.EqualValues(t, 100, snapshot.ReservedBalance)
	var resetCredits int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ? AND reason_code = ?", account.AccountID, "subscription_quota_reset").Count(&resetCredits).Error)
	assert.EqualValues(t, 1, resetCredits)
}
