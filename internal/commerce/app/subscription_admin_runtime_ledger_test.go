package app

import (
	"errors"
	"fmt"
	"testing"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func adminQuotaFixture(t *testing.T, total, used int64, materialize bool) (*gorm.DB, *commerceschema.UserSubscription, string) {
	t.Helper()
	db := setupRedemptionTestDB(t)
	plan := insertSubscriptionResetAppTestPlan(t, 18501, 0, total)
	plan.QuotaResetPeriod = commerceschema.SubscriptionResetNever
	require.NoError(t, db.Save(plan).Error)
	sub := &commerceschema.UserSubscription{
		Id: 18502, UserId: 18503, PlanId: plan.Id, AmountTotal: total, AmountUsed: used,
		StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix(), Status: "active",
	}
	require.NoError(t, db.Create(sub).Error)
	if !materialize {
		return db, sub, ""
	}
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota",
	})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: total - used, IdempotencyKey: "admin-quota-fixture", ReasonCode: "test",
	})
	require.NoError(t, err)
	return db, sub, account.AccountID
}

func adminQuotaInput(sub *commerceschema.UserSubscription, total, used int64) adminUpdateUserSubscriptionRuntimeInput {
	return adminUpdateUserSubscriptionRuntimeInput{
		StartTime: sub.StartTime, EndTime: sub.EndTime, Status: sub.Status, AmountTotal: total, AmountUsed: used,
		PeriodAmount: sub.PeriodAmount, PeriodUsed: sub.PeriodUsed, ModelLimits: sub.ModelLimits,
	}
}

func adminQuotaSnapshot(t *testing.T, db *gorm.DB, accountID string, available, reserved int64) {
	t.Helper()
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&snapshot, "account_id = ?", accountID).Error)
	require.Equal(t, available, snapshot.AvailableBalance)
	require.Equal(t, reserved, snapshot.ReservedBalance)
}

func TestAdminRuntimeQuotaAdjustmentSurvivesReconciliationAndReplay(t *testing.T) {
	db, sub, accountID := adminQuotaFixture(t, 1000, 200, true)
	for _, target := range [][2]int64{{1200, 200}, {700, 300}, {1200, 200}, {700, 300}} {
		_, err := updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, target[0], target[1]))
		require.NoError(t, err)
		adminQuotaSnapshot(t, db, accountID, target[0]-target[1], 0)
		changed, err := reconcileSubscriptionLedgerProjection(sub.Id)
		require.NoError(t, err)
		require.False(t, changed)
		var before, after int64
		require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&before).Error)
		_, err = updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, target[0], target[1]))
		require.NoError(t, err)
		require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&after).Error)
		require.Equal(t, before, after, "same target cannot adjust funds again")
	}
	var stored commerceschema.UserSubscription
	require.NoError(t, db.First(&stored, sub.Id).Error)
	require.EqualValues(t, 700, stored.AmountTotal)
	require.EqualValues(t, 300, stored.AmountUsed)
}

func TestAdminRuntimeQuotaTargetRepairsStaleProjectionEvenWhenInputIsUnchanged(t *testing.T) {
	db, sub, accountID := adminQuotaFixture(t, 1000, 200, true)
	require.NoError(t, db.Model(sub).Update("amount_used", 100).Error)
	_, err := updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, 1000, 100))
	require.NoError(t, err)
	adminQuotaSnapshot(t, db, accountID, 900, 0)
	changed, err := reconcileSubscriptionLedgerProjection(sub.Id)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestAdminRuntimeQuotaAdjustmentPreservesReservedFunds(t *testing.T) {
	db, sub, accountID := adminQuotaFixture(t, 1000, 200, true)
	_, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{
		AccountID: accountID, RequestID: "admin-quota-held-request", ReservedAmount: 100,
		IdempotencyKey: "admin-quota-held-request:reserve",
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(sub).Update("amount_used", 300).Error)
	_, err = updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, 1100, 350))
	require.NoError(t, err)
	adminQuotaSnapshot(t, db, accountID, 750, 100)
	for _, invalid := range [][2]int64{{1100, 99}, {99, 100}, {0, 0}} {
		_, err = updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, invalid[0], invalid[1]))
		require.Error(t, err)
		adminQuotaSnapshot(t, db, accountID, 750, 100)
		var stored commerceschema.UserSubscription
		require.NoError(t, db.First(&stored, sub.Id).Error)
		require.EqualValues(t, 1100, stored.AmountTotal)
		require.EqualValues(t, 350, stored.AmountUsed)
	}
}

func TestAdminRuntimeQuotaAdjustmentBootstrapsLegacyBalance(t *testing.T) {
	for _, tt := range []struct {
		name                        string
		oldUsed, newUsed, available int64
	}{{"remaining", 200, 450, 550}, {"exhausted", 1000, 900, 100}} {
		t.Run(tt.name, func(t *testing.T) {
			db, sub, _ := adminQuotaFixture(t, 1000, tt.oldUsed, false)
			_, err := updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, 1000, tt.newUsed))
			require.NoError(t, err)
			var account billingschema.BillingAccount
			require.NoError(t, db.First(&account, "owner_type = ? AND owner_id = ?", "user_subscription", sub.Id).Error)
			adminQuotaSnapshot(t, db, account.AccountID, tt.available, 0)
			var bootstrap billingschema.BillingLedgerEntry
			require.NoError(t, db.First(&bootstrap, "account_id = ? AND reason_code = ?", account.AccountID, "subscription_balance_bootstrap").Error)
			require.Equal(t, 1000-tt.oldUsed, bootstrap.Amount)
			changed, err := reconcileSubscriptionLedgerProjection(sub.Id)
			require.NoError(t, err)
			require.False(t, changed)
		})
	}
}

func TestAdminRuntimeQuotaAdjustmentCanExhaustBalance(t *testing.T) {
	db, sub, accountID := adminQuotaFixture(t, 1000, 200, true)
	_, err := updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, 700, 700))
	require.NoError(t, err)
	adminQuotaSnapshot(t, db, accountID, 0, 0)
	changed, err := reconcileSubscriptionLedgerProjection(sub.Id)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestAdminRuntimeQuotaAdjustmentRejectsInvalidQuotaWithoutWrites(t *testing.T) {
	for _, invalid := range [][2]int64{{1000, -1}, {-1, 0}, {1000, 1001}, {0, 0}} {
		t.Run(fmt.Sprintf("%d_%d", invalid[0], invalid[1]), func(t *testing.T) {
			db, sub, _ := adminQuotaFixture(t, 1000, 200, false)
			_, err := updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, invalid[0], invalid[1]))
			require.Error(t, err)
			var accounts, entries int64
			require.NoError(t, db.Model(&billingschema.BillingAccount{}).Count(&accounts).Error)
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&entries).Error)
			require.Zero(t, accounts)
			require.Zero(t, entries)
			var stored commerceschema.UserSubscription
			require.NoError(t, db.First(&stored, sub.Id).Error)
			require.EqualValues(t, 1000, stored.AmountTotal)
			require.EqualValues(t, 200, stored.AmountUsed)
		})
	}
}

func TestAdminRuntimeMetadataUpdateKeepsLegacyBalanceUnmaterialized(t *testing.T) {
	db, sub, _ := adminQuotaFixture(t, 1000, 200, false)
	input := adminQuotaInput(sub, 1000, 200)
	input.EndTime++
	_, err := updateAdminUserSubscriptionRuntime(sub.Id, input)
	require.NoError(t, err)
	var accounts int64
	require.NoError(t, db.Model(&billingschema.BillingAccount{}).Count(&accounts).Error)
	require.Zero(t, accounts)
}

func TestAdminRuntimeQuotaAdjustmentKeepsCreditPackPurchaseSnapshot(t *testing.T) {
	db, sub, accountID := adminQuotaFixture(t, 1050, 200, true)
	sub.CreditPackGrantedQuota, sub.CreditPackPaidQuota, sub.CreditPackChannelShareBPS = 1050, 1000, 6000
	sub.EndTime = 0
	require.NoError(t, db.Save(sub).Error)
	_, err := updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, 1100, 200))
	require.EqualError(t, err, "permanent credit pack purchased quota cannot be changed")
	adminQuotaSnapshot(t, db, accountID, 850, 0)
	_, err = updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, 1050, 100))
	require.NoError(t, err)
	adminQuotaSnapshot(t, db, accountID, 950, 0)
	var stored commerceschema.UserSubscription
	require.NoError(t, db.First(&stored, sub.Id).Error)
	require.EqualValues(t, 1050, stored.CreditPackGrantedQuota)
	require.EqualValues(t, 1000, stored.CreditPackPaidQuota)
	require.EqualValues(t, 6000, stored.CreditPackChannelShareBPS)
	require.Zero(t, stored.EndTime)
}

func TestAdminRuntimeQuotaAdjustmentRollsBackWhenProjectionSaveFails(t *testing.T) {
	db, sub, accountID := adminQuotaFixture(t, 1000, 200, true)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail_admin_subscription_save", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "UserSubscription" {
			tx.AddError(errors.New("test projection failure"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove("fail_admin_subscription_save") })
	_, err := updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, 1200, 100))
	require.EqualError(t, err, "test projection failure")
	adminQuotaSnapshot(t, db, accountID, 800, 0)
	var entries int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&entries).Error)
	require.EqualValues(t, 1, entries)
	var stored commerceschema.UserSubscription
	require.NoError(t, db.First(&stored, sub.Id).Error)
	require.EqualValues(t, 1000, stored.AmountTotal)
	require.EqualValues(t, 200, stored.AmountUsed)
}

func TestAdminRuntimeLifecycleUsesRowLockClause(t *testing.T) {
	for _, operation := range []string{"update", "invalidate", "delete", "reset"} {
		t.Run(operation, func(t *testing.T) {
			db, sub, _ := adminQuotaFixture(t, 1000, 200, false)
			locked := false
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("check_admin_subscription_lock", func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "UserSubscription" {
					if lock, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking); ok && lock.Strength == "UPDATE" {
						locked = true
					}
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Query().Remove("check_admin_subscription_lock") })
			var err error
			switch operation {
			case "update":
				_, err = updateAdminUserSubscriptionRuntime(sub.Id, adminQuotaInput(sub, 1000, 200))
			case "invalidate":
				_, err = invalidateAdminUserSubscriptionRuntime(sub.Id)
			case "delete":
				_, err = deleteAdminUserSubscriptionRuntime(sub.Id)
			case "reset":
				_, err = resetAdminUserSubscriptionQuotaRuntime(sub.Id, adminResetUserSubscriptionQuotaRuntimeInput{})
			}
			require.NoError(t, err)
			require.True(t, locked)
		})
	}
}
