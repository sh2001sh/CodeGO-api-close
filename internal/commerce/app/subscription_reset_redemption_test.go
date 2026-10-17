package app

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupResetRedemption(t *testing.T, available int) (*gorm.DB, int, commerceschema.SubscriptionPlan) {
	t.Helper()
	db := setupRedemptionTestDB(t)
	userID := 74001
	insertSubscriptionResetAppTestUser(t, userID, 100)
	plans := defaultSubscriptionPlans()
	for i := range plans {
		require.NoError(t, db.Create(&plans[i]).Error)
	}
	require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityAccount{UserId: userID, EarnedTotal: available + 1, UsedTotal: 1, AvailableTotal: available, LastUsedMonth: "2020-01"}).Error)
	return db, userID, plans[0]
}

func TestResetRedemptionConsumesOnePerGrantAndReplaysOriginalSnapshot(t *testing.T) {
	db, userID, plan := setupResetRedemption(t, 2)
	preview, err := BuildSubscriptionResetRedemptionPreview(userID)
	require.NoError(t, err)
	require.True(t, preview.Eligible)
	require.Equal(t, plan.Id, preview.PlanID)
	require.Equal(t, plan.TotalAmount, preview.GrantedQuota)
	first, err := RedeemSubscriptionResetOpportunity(userID, RedeemSubscriptionResetOpportunityRequest{RequestID: "first"})
	require.NoError(t, err)
	require.Equal(t, 1, first.ResetOpportunity.AvailableCount)
	require.Equal(t, "2020-01", first.ResetOpportunity.LastUsedMonth)
	require.False(t, first.Replayed)
	// Catalog revisions cannot alter an already-granted redemption on replay.
	require.NoError(t, db.Model(&plan).Updates(map[string]any{"enabled": false, "total_amount": 1}).Error)
	replay, err := RedeemSubscriptionResetOpportunity(userID, RedeemSubscriptionResetOpportunityRequest{RequestID: "first"})
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, first.SubscriptionID, replay.SubscriptionID)
	require.Equal(t, first.GrantedQuota, replay.GrantedQuota)
	second, err := RedeemSubscriptionResetOpportunity(userID, RedeemSubscriptionResetOpportunityRequest{RequestID: "second"})
	require.NoError(t, err)
	require.NotEqual(t, first.SubscriptionID, second.SubscriptionID)
	require.Equal(t, 0, second.ResetOpportunity.AvailableCount)
	_, err = RedeemSubscriptionResetOpportunity(userID, RedeemSubscriptionResetOpportunityRequest{RequestID: "third"})
	require.ErrorIs(t, err, commerceschema.ErrSubscriptionResetOpportunityUnavailable)
	var sub commerceschema.UserSubscription
	require.NoError(t, db.First(&sub, first.SubscriptionID).Error)
	require.Zero(t, sub.EndTime)
	require.Equal(t, "reset_redemption", sub.Source)
	require.Equal(t, first.GrantedQuota, sub.CreditPackGrantedQuota)
	require.Equal(t, sub.CreditPackGrantedQuota, sub.CreditPackPaidQuota)
	for _, model := range []any{&commerceschema.SubscriptionOrder{}, &commerceschema.TopUp{}} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.Zero(t, count, "redemption is not a refundable cash payment")
	}
	var useCount int64
	require.NoError(t, db.Model(&commerceschema.SubscriptionResetOpportunityLedger{}).Where("change_type = ?", "use").Count(&useCount).Error)
	require.Zero(t, useCount, "redemption must not create historical subscription refresh usage")
}

func TestResetRedemptionNoEnabledCatalogLeavesOpportunityUntouched(t *testing.T) {
	db, userID, _ := setupResetRedemption(t, 1)
	require.NoError(t, db.Model(&commerceschema.SubscriptionPlan{}).Where("1 = 1").Update("enabled", false).Error)
	preview, err := BuildSubscriptionResetRedemptionPreview(userID)
	require.NoError(t, err)
	require.False(t, preview.Eligible)
	_, err = RedeemSubscriptionResetOpportunity(userID, RedeemSubscriptionResetOpportunityRequest{RequestID: "empty"})
	require.Error(t, err)
	var account commerceschema.SubscriptionResetOpportunityAccount
	require.NoError(t, db.Where("user_id = ?", userID).First(&account).Error)
	require.Equal(t, 1, account.AvailableTotal)
}

func TestResetRedemptionCatalogExclusionsTieAndZeroBalancePreview(t *testing.T) {
	db, userID, plan := setupResetRedemption(t, 0)
	for i, updates := range []map[string]any{
		{"price_amount": 1, "enabled": false},
		{"price_amount": 1, "internal_only": true},
		{"price_amount": 0},
		{"price_amount": 1, "total_amount": 0},
		{"price_amount": 1, "plan_type": commerceschema.SubscriptionPlanTypeMonthly},
		{"price_amount": plan.PriceAmount},
	} {
		candidate := plan
		candidate.Id = 80000 + i
		require.NoError(t, db.Create(&candidate).Error)
		require.NoError(t, db.Model(&candidate).Updates(updates).Error)
	}
	preview, err := BuildSubscriptionResetRedemptionPreview(userID)
	require.NoError(t, err)
	require.Equal(t, plan.Id, preview.PlanID, "same-price plans choose the lowest ID")
	require.False(t, preview.Eligible)
	require.Zero(t, preview.AvailableCount)
	for _, req := range []RedeemSubscriptionResetOpportunityRequest{{}, {RequestID: string(make([]byte, 129))}} {
		_, err := RedeemSubscriptionResetOpportunity(userID, req)
		require.Error(t, err)
	}
}

func TestResetRedemptionFailureRollsBackGiftAndOpportunity(t *testing.T) {
	db, userID, _ := setupResetRedemption(t, 1)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:reset-redemption-fail", func(tx *gorm.DB) {
		if ledger, ok := tx.Statement.Dest.(*commerceschema.SubscriptionResetOpportunityLedger); ok && ledger.ChangeType == "redeem" {
			tx.AddError(errors.New("injected redemption receipt failure"))
		}
	}))
	_, err := RedeemSubscriptionResetOpportunity(userID, RedeemSubscriptionResetOpportunityRequest{RequestID: "rollback"})
	require.ErrorContains(t, err, "injected redemption")
	var account commerceschema.SubscriptionResetOpportunityAccount
	require.NoError(t, db.Where("user_id = ?", userID).First(&account).Error)
	require.Equal(t, 1, account.AvailableTotal)
	require.Equal(t, 1, account.UsedTotal)
	for _, model := range []any{&commerceschema.UserSubscription{}, &commerceschema.SubscriptionResetOpportunityLedger{}, &billingschema.BillingAccount{}, &billingschema.BillingLedgerEntry{}} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestResetRedemptionConcurrentDistinctRequestsCannotOverspend(t *testing.T) {
	db, userID, _ := setupResetRedemption(t, 2)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1) // SQLite writer serialization; not a PG lock claim.
	const callers = 12
	start := make(chan struct{})
	errors := make(chan error, callers)
	var workers sync.WaitGroup
	for i := range callers {
		workers.Go(func() {
			<-start
			_, err := RedeemSubscriptionResetOpportunity(userID, RedeemSubscriptionResetOpportunityRequest{RequestID: fmt.Sprintf("concurrent-%d", i)})
			errors <- err
		})
	}
	close(start)
	workers.Wait()
	close(errors)
	success := 0
	for err := range errors {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, commerceschema.ErrSubscriptionResetOpportunityUnavailable)
		}
	}
	require.Equal(t, 2, success)
	var count int64
	require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("source = ?", "reset_redemption").Count(&count).Error)
	require.EqualValues(t, 2, count)
}
