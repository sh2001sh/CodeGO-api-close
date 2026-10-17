package app

import (
	"errors"
	"fmt"
	"math"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRealtimeSubscriptionHoldsAndAggregateSettleMatchLedger(t *testing.T) {
	db, sub, _ := adminQuotaFixture(t, 10000, 0, false)
	ensureSubscriptionPreConsumeRecordSchema(t)
	insertSubscriptionStoreTestUser(t, sub.UserId, []int{sub.Id})
	policy := gatewaystore.SubscriptionGroupPolicy2JSONString()
	require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(`{"realtime_test":{"enabled":true,"multiplier":1}}`))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(policy)) })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: sub.UserId, RequestId: "realtime-ledger-aggregate", OriginModelName: "gpt-5",
		UsingGroup: "realtime_test", IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "subscription_only"},
		PriceData:   types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	session, apiErr := billingapp.NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	info.Billing = session
	usage := &dto.RealtimeUsage{InputTokenDetails: dto.InputTokenDetails{TextTokens: 100}}
	modelRatio, _, _ := gatewaystore.GetModelRatio(info.OriginModelName)
	chunk := int(math.Round(100 * modelRatio * gatewaystore.GetGroupRatio(info.UsingGroup)))
	require.Positive(t, chunk)
	require.NoError(t, billingapp.PreWssConsumeQuota(ctx, info, usage))
	require.NoError(t, billingapp.PreWssConsumeQuota(ctx, info, usage))
	var account billingschema.BillingAccount
	require.NoError(t, db.First(&account, "owner_type = ? AND owner_id = ?", "user_subscription", sub.Id).Error)
	adminQuotaSnapshot(t, db, account.AccountID, int64(10000-max(100, chunk*2)), int64(max(100, chunk*2)))
	var before billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&before, "account_id = ?", account.AccountID).Error)
	require.Zero(t, before.ConsumedTotal)
	require.NoError(t, session.Settle(chunk*2))
	require.NoError(t, session.Settle(chunk*2))
	adminQuotaSnapshot(t, db, account.AccountID, int64(10000-chunk*2), 0)
	var after billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&after, "account_id = ?", account.AccountID).Error)
	require.EqualValues(t, chunk*2, after.ConsumedTotal)
	var stored commerceschema.UserSubscription
	require.NoError(t, db.First(&stored, sub.Id).Error)
	require.EqualValues(t, chunk*2, stored.AmountUsed)
	changed, err := reconcileSubscriptionLedgerProjection(sub.Id)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestSubscriptionUsageAdjustmentLedgerReplayAndReconciliation(t *testing.T) {
	db, sub, _ := adminQuotaFixture(t, 1000, 200, false)
	for _, change := range []struct {
		key              string
		delta, available int64
	}{
		{"debit", 100, 700}, {"refund", -50, 750},
	} {
		applied, err := AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", change.delta, change.key)
		require.NoError(t, err)
		require.True(t, applied)
		applied, err = AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", change.delta, change.key)
		require.NoError(t, err)
		require.False(t, applied)
		_, err = AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", change.delta+1, change.key)
		require.ErrorIs(t, err, billingdomain.ErrLedgerConflict)
		_, err = AdjustUserSubscriptionUsageDelta(sub.Id, "other-model", change.delta, change.key)
		require.ErrorIs(t, err, billingdomain.ErrLedgerConflict)
		var account billingschema.BillingAccount
		require.NoError(t, db.First(&account, "owner_type = ? AND owner_id = ?", "user_subscription", sub.Id).Error)
		adminQuotaSnapshot(t, db, account.AccountID, change.available, 0)
		changed, err := reconcileSubscriptionLedgerProjection(sub.Id)
		require.NoError(t, err)
		require.False(t, changed)
	}
	var stored commerceschema.UserSubscription
	require.NoError(t, db.First(&stored, sub.Id).Error)
	require.EqualValues(t, 250, stored.AmountUsed)
}

func TestSubscriptionUsageAdjustmentPreservesHeldQuotaAndRejectsBounds(t *testing.T) {
	db, sub, accountID := adminQuotaFixture(t, 1000, 200, true)
	_, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{
		AccountID: accountID, RequestID: "adjustment-unrelated-hold", ReservedAmount: 100,
		IdempotencyKey: "adjustment-unrelated-hold:reserve",
	})
	require.NoError(t, err)
	// The authoritative snapshot overrides a stale compatibility projection.
	require.NoError(t, db.Model(sub).Update("amount_used", 999).Error)
	for index, delta := range []int64{-201, 701, math.MinInt64} {
		_, err := AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", delta, string(rune('a'+index)))
		require.Error(t, err)
		adminQuotaSnapshot(t, db, accountID, 700, 100)
	}
	_, err = AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", -200, "refund-settled-only")
	require.NoError(t, err)
	adminQuotaSnapshot(t, db, accountID, 900, 100)
	var stored commerceschema.UserSubscription
	require.NoError(t, db.First(&stored, sub.Id).Error)
	require.EqualValues(t, 100, stored.AmountUsed)
	_, err = AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", -1, "cannot-refund-hold")
	require.Error(t, err)
	adminQuotaSnapshot(t, db, accountID, 900, 100)
}

func TestSubscriptionUsageAdjustmentRollsBackLedgerOnProjectionFailure(t *testing.T) {
	db, sub, accountID := adminQuotaFixture(t, 1000, 200, true)
	var ledgerBefore, outboxBefore int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&ledgerBefore).Error)
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Count(&outboxBefore).Error)
	const callback = "test:usage-adjustment-projection-failure"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "user_subscriptions" {
			tx.AddError(errors.New("injected usage projection failure"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	applied, err := AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", 100, "retry-after-rollback")
	require.ErrorContains(t, err, "injected usage projection failure")
	require.False(t, applied)
	adminQuotaSnapshot(t, db, accountID, 800, 0)
	var ledgerAfter, outboxAfter int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&ledgerAfter).Error)
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Count(&outboxAfter).Error)
	require.Equal(t, ledgerBefore, ledgerAfter)
	require.Equal(t, outboxBefore, outboxAfter)
	require.NoError(t, db.Callback().Update().Remove(callback))
	applied, err = AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", 100, "retry-after-rollback")
	require.NoError(t, err)
	require.True(t, applied)
	adminQuotaSnapshot(t, db, accountID, 700, 0)
}

func TestSubscriptionUsageAdjustmentRejectsInvalidLegacyProjection(t *testing.T) {
	for _, used := range []int64{-1, 1001, math.MinInt64} {
		t.Run(fmt.Sprint(used), func(t *testing.T) {
			db, sub, _ := adminQuotaFixture(t, 1000, used, false)
			_, err := AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", 1, "invalid-projection")
			require.Error(t, err)
			var count int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestSubscriptionUsageAdjustmentKeepsUnlimitedUsageReplayable(t *testing.T) {
	db, sub, _ := adminQuotaFixture(t, 0, 200, false)
	for _, change := range []struct {
		key   string
		delta int64
	}{{"unlimited-consume", 50}, {"unlimited-refund", -100}} {
		applied, err := AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", change.delta, change.key)
		require.NoError(t, err)
		require.True(t, applied)
		applied, err = AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", change.delta, change.key)
		require.NoError(t, err)
		require.False(t, applied)
	}
	var stored commerceschema.UserSubscription
	require.NoError(t, db.First(&stored, sub.Id).Error)
	require.Zero(t, stored.AmountTotal)
	require.EqualValues(t, 150, stored.AmountUsed)
	_, err := AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", -151, "unlimited-excess-refund")
	require.Error(t, err)
	var receipts int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("reason_code = ?", "subscription_unlimited_usage_adjustment").Count(&receipts).Error)
	require.EqualValues(t, 2, receipts)
}
