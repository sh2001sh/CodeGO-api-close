package app

import (
	"encoding/json"
	"testing"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformschema "github.com/sh2001sh/new-api/internal/platform/schema"
	"github.com/stretchr/testify/require"
)

func TestApplyUnifiedCreditMigrationPreservesMonthlyPassAndConvertsLegacyGPTBalanceOnce(t *testing.T) {
	db := setupRedemptionTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&commerceschema.UnifiedCreditUserMigration{},
		&commerceschema.SubscriptionTierSettlement{},
	))
	quotaUnit := int64(platformruntime.QuotaPerUnit)
	user := identityschema.User{Id: 9971, Username: "unified-migration", Quota: int(400 * quotaUnit), ClaudeQuota: int(10 * quotaUnit)}
	require.NoError(t, db.Create(&user).Error)
	plan := commerceschema.SubscriptionPlan{
		Id: 771, Title: "Standard月卡", PlanType: commerceschema.SubscriptionPlanTypeMonthly,
		MembershipTier: commerceschema.SubscriptionMembershipTierStandard, Enabled: true,
	}
	require.NoError(t, db.Create(&plan).Error)
	subscription := commerceschema.UserSubscription{
		Id: 881, UserId: user.Id, PlanId: plan.Id, Source: "manual",
		AmountTotal: 650 * quotaUnit, AmountUsed: 260 * quotaUnit,
		StartTime: platformruntime.GetTimestamp() - 100, EndTime: platformruntime.GetTimestamp() + 10_000, Status: "active",
	}
	require.NoError(t, db.Create(&subscription).Error)

	summary, err := ApplyUnifiedCreditMigration()
	require.NoError(t, err)
	require.Equal(t, int64(1), summary.UsersPending)
	require.Zero(t, summary.SubscriptionsPending)
	require.Zero(t, summary.SubscriptionsNeedReview)
	require.Equal(t, int64(400*quotaUnit), summary.LegacyGPTQuota)
	require.Equal(t, int64(100*quotaUnit), summary.ConvertedUnifiedQuota)
	require.Zero(t, summary.SubscriptionUnifiedQuota)
	_, err = ApplyUnifiedCreditMigration()
	require.NoError(t, err)

	var reloaded identityschema.User
	require.NoError(t, db.First(&reloaded, user.Id).Error)
	require.Zero(t, reloaded.Quota)
	require.Equal(t, int(110*quotaUnit), reloaded.ClaudeQuota)

	var settled commerceschema.UserSubscription
	require.NoError(t, db.First(&settled, subscription.Id).Error)
	require.Equal(t, "active", settled.Status)
	require.Equal(t, int64(260*quotaUnit), settled.AmountUsed)

	var settlementCount int64
	require.NoError(t, db.Model(&commerceschema.SubscriptionTierSettlement{}).
		Where("user_subscription_id = ?", subscription.Id).Count(&settlementCount).Error)
	require.Zero(t, settlementCount)

	var reloadedPlan commerceschema.SubscriptionPlan
	require.NoError(t, db.First(&reloadedPlan, plan.Id).Error)
	require.True(t, reloadedPlan.Enabled)

	var version platformschema.Option
	require.NoError(t, db.First(&version, "key = ?", unifiedCreditSchemaVersionKey).Error)
	require.Equal(t, commerceschema.UnifiedCreditMigrationVersion, version.Value)
}

func TestApplyUnifiedCreditMigrationUsesSunbornSpecialRate(t *testing.T) {
	db := setupRedemptionTestDB(t)
	require.NoError(t, db.AutoMigrate(&commerceschema.UnifiedCreditUserMigration{}))
	quotaUnit := int64(platformruntime.QuotaPerUnit)
	sunborn := identityschema.User{Id: 9974, Username: "sunborn", AffCode: "sunborn-rate", Quota: int(100 * quotaUnit)}
	regular := identityschema.User{Id: 9975, Username: "regular-user", AffCode: "regular-rate", Quota: int(100 * quotaUnit)}
	require.NoError(t, db.Create(&sunborn).Error)
	require.NoError(t, db.Create(&regular).Error)

	summary, err := ApplyUnifiedCreditMigration()
	require.NoError(t, err)
	require.EqualValues(t, 2, summary.UsersPending)
	require.EqualValues(t, 11*quotaUnit, summary.SpecialRateConvertedQuota)
	require.EqualValues(t, 1, summary.SpecialRateUsers)
	require.EqualValues(t, 36*quotaUnit, summary.ConvertedUnifiedQuota)

	var migratedSunborn commerceschema.UnifiedCreditUserMigration
	require.NoError(t, db.Where("user_id = ?", sunborn.Id).First(&migratedSunborn).Error)
	require.EqualValues(t, 11*quotaUnit, migratedSunborn.ConvertedUnifiedQuota)
}

func TestApplyUnifiedCreditMigrationIgnoresInvalidMonthlyPassUsage(t *testing.T) {
	db := setupRedemptionTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&commerceschema.UnifiedCreditUserMigration{},
		&commerceschema.SubscriptionTierSettlement{},
	))
	user := identityschema.User{Id: 9972, Username: "unified-review"}
	require.NoError(t, db.Create(&user).Error)
	plan := commerceschema.SubscriptionPlan{
		Id: 772, Title: "Pro月卡", PlanType: commerceschema.SubscriptionPlanTypeMonthly,
		MembershipTier: commerceschema.SubscriptionMembershipTierPro, Enabled: true,
	}
	require.NoError(t, db.Create(&plan).Error)
	subscription := commerceschema.UserSubscription{
		Id: 882, UserId: user.Id, PlanId: plan.Id, AmountTotal: 100, AmountUsed: 101,
		EndTime: platformruntime.GetTimestamp() + 10_000, Status: "active",
	}
	require.NoError(t, db.Create(&subscription).Error)

	_, err := ApplyUnifiedCreditMigration()
	require.NoError(t, err)

	var reloaded commerceschema.UserSubscription
	require.NoError(t, db.First(&reloaded, subscription.Id).Error)
	require.Equal(t, "active", reloaded.Status)
	require.Equal(t, int64(101), reloaded.AmountUsed)

	var version platformschema.Option
	require.NoError(t, db.First(&version, "key = ?", unifiedCreditSchemaVersionKey).Error)
	require.Equal(t, commerceschema.UnifiedCreditMigrationVersion, version.Value)
}

func TestMigrateLegacyPaidBlindBoxOrdersIssuesRemainingInventoryOnce(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := identityschema.User{Id: 9973, Username: "legacy-paid-box"}
	require.NoError(t, db.Create(&user).Error)
	order := commerceschema.BlindBoxOrder{
		Id: 991, UserId: user.Id, Quantity: 5, OpenedCount: 2, Money: 12.5,
		TradeNo: "legacy-paid-box-991", Status: "success",
	}
	require.NoError(t, db.Create(&order).Error)

	require.NoError(t, migrateLegacyPaidBlindBoxOrdersToInventory())
	require.NoError(t, migrateLegacyPaidBlindBoxOrdersToInventory())

	var itemCount int64
	require.NoError(t, db.Model(&commerceschema.BalanceBlindBoxItem{}).
		Where("owner_user_id = ? AND status = ?", user.Id, commerceschema.BalanceBlindBoxItemStatusAvailable).
		Count(&itemCount).Error)
	require.Equal(t, int64(3), itemCount)

	var purchase commerceschema.BalanceBlindBoxPurchase
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&purchase).Error)
	require.Equal(t, 3, purchase.Quantity)

	require.NoError(t, db.First(&order, order.Id).Error)
	require.Equal(t, order.Quantity, order.OpenedCount)
}

func TestMigrateUnifiedCreditGPTGroupRatiosOnlyScalesOfficialGPTGroupsOnce(t *testing.T) {
	db := setupRedemptionTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&gatewayschema.Channel{},
		&commerceschema.UnifiedCreditGroupRatioMigration{},
	))
	originalRatios := gatewaystore.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(originalRatios))
	})
	originalPolicies := gatewaystore.SubscriptionGroupPolicy2JSONString()
	t.Cleanup(func() {
		require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(originalPolicies))
	})

	claudeSetting := `{"claude_wallet_enabled":true}`
	channels := []gatewayschema.Channel{
		{Id: 31, Key: "official-gpt", ChannelScope: gatewayschema.ChannelScopeOfficial, Group: "gpt-official"},
		{Id: 32, Key: "official-claude", ChannelScope: gatewayschema.ChannelScopeOfficial, Group: "claude-wallet", Setting: &claudeSetting},
		{Id: 33, Key: "external-gpt", ChannelScope: gatewayschema.ChannelScopeExternal, Group: "marketplace-external"},
		{Id: 34, Key: "plus", ChannelScope: gatewayschema.ChannelScopeOfficial, Group: "Plus分组"},
		{Id: 35, Key: "pro", ChannelScope: gatewayschema.ChannelScopeOfficial, Group: "纯Pro号池"},
	}
	require.NoError(t, db.Create(&channels).Error)
	ratioJSON := `{"gpt-official":1.2,"claude-wallet":0.8,"marketplace-external":1.6,"Plus分组":1,"纯Pro号池":1.5}`
	require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(ratioJSON))
	require.NoError(t, db.Create(&platformschema.Option{Key: "GroupRatio", Value: ratioJSON}).Error)

	require.NoError(t, migrateUnifiedCreditGPTGroupRatios())
	require.NoError(t, migrateUnifiedCreditGPTGroupRatios())

	var option platformschema.Option
	require.NoError(t, db.First(&option, "key = ?", "GroupRatio").Error)
	var ratios map[string]float64
	require.NoError(t, json.Unmarshal([]byte(option.Value), &ratios))
	require.InDelta(t, 0.3, ratios["gpt-official"], 0.000001)
	require.InDelta(t, 0.8, ratios["claude-wallet"], 0.000001)
	require.InDelta(t, 1.6, ratios["marketplace-external"], 0.000001)
	require.InDelta(t, 0.10, ratios["Plus分组"], 0.000001)
	require.InDelta(t, 0.16, ratios["纯Pro号池"], 0.000001)

	var audits []commerceschema.UnifiedCreditGroupRatioMigration
	require.NoError(t, db.Find(&audits).Error)
	require.Len(t, audits, 3)

	var policy platformschema.Option
	require.NoError(t, db.First(&policy, "key = ?", gatewaystore.SubscriptionGroupPolicyOptionKey).Error)
	var policies map[string]gatewaystore.SubscriptionGroupPolicy
	require.NoError(t, json.Unmarshal([]byte(policy.Value), &policies))
	require.Equal(t, gatewaystore.SubscriptionGroupPolicy{Enabled: true, Multiplier: 1}, policies["Plus分组"])
	require.Equal(t, gatewaystore.SubscriptionGroupPolicy{Enabled: true, Multiplier: 1.5}, policies["纯Pro号池"])
}
