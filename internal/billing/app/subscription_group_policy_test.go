package app

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/dto"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupMonthlyPassFundingTestDB(t *testing.T) {
	t.Helper()
	originalDB := platformdb.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })
	require.NoError(t, db.AutoMigrate(
		&identityschema.User{},
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{},
	))
}

func testMonthlyPassEntitlement(int) (*MonthlyPassEntitlement, error) {
	return &MonthlyPassEntitlement{PropID: 77, Multiplier: 0.1, ExpiresAt: 4_102_444_800}, nil
}

func testValidMonthlyPassEntitlement(int, MonthlyPassEntitlement) (bool, error) {
	return true, nil
}

func TestSubscriptionGroupPolicyAppliesToPreConsumeAndSettlement(t *testing.T) {
	originalPolicy := gatewaystore.SubscriptionGroupPolicy2JSONString()
	require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString("{\"official\":{\"enabled\":true,\"multiplier\":0.5}}"))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(originalPolicy)) })

	previousHooks := subscriptionFundingHooks
	var preConsumed int64
	var settledDelta int64
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsume: func(_ string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
			preConsumed = amount
			return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 99, PreConsumed: amount, AmountTotal: 10_000, AmountUsedAfter: amount}, nil
		},
		PostConsumeDelta: func(_ int, _ string, delta int64) error {
			settledDelta = delta
			return nil
		},
	})
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(previousHooks) })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 1, OriginModelName: "gpt-5", RequestId: "subscription-policy-scale",
		UsingGroup: "official", ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeOfficial},
		IsPlayground: true, ForcePreConsume: true,
		PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}},
	}

	session, apiErr := NewBillingSession(ctx, info, 1_000)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, int64(250), preConsumed)
	require.Equal(t, 250, session.GetPreConsumedQuota())
	require.Equal(t, BillingSourceSubscription, info.BillingSource)
	require.Equal(t, 0.5, info.SubscriptionGroupMultiplier)
	require.Equal(t, 0.25, info.SubscriptionQuotaScale)
	require.Equal(t, 2.0, info.SubscriptionGroupRatio)

	require.NoError(t, session.Settle(1_600))
	require.Equal(t, int64(150), settledDelta)
	require.Equal(t, 400, BillingQuotaForLog(info, 1_600))
}

func TestMonthlyPassMultiplierAppliesToSubscriptionFunding(t *testing.T) {
	originalPolicy := gatewaystore.SubscriptionGroupPolicy2JSONString()
	require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString("{\"official\":{\"enabled\":true,\"multiplier\":1}}"))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(originalPolicy)) })

	previousHooks := subscriptionFundingHooks
	var preConsumed int64
	var settledDelta int64
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsume: func(_ string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
			preConsumed = amount
			return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 199, PreConsumed: amount, AmountTotal: 10_000, AmountUsedAfter: amount}, nil
		},
		PostConsumeDelta: func(_ int, _ string, delta int64) error {
			settledDelta = delta
			return nil
		},
		GetMonthlyPassEntitlement:      testMonthlyPassEntitlement,
		ValidateMonthlyPassEntitlement: testValidMonthlyPassEntitlement,
	})
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(previousHooks) })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 2, OriginModelName: "gpt-5", RequestId: "monthly-pass-subscription-funded",
		UsingGroup: "official", ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeOfficial},
		IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{FundingSourceOrder: []string{BillingSourceSubscription, BillingSourceWallet}},
		PriceData:   types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}

	session, apiErr := NewBillingSession(ctx, info, 1_000)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, int64(1_000), preConsumed)
	require.Equal(t, 1_000, session.GetPreConsumedQuota())
	require.Equal(t, BillingSourceSubscription, info.BillingSource)
	require.Equal(t, 1.0, info.SubscriptionGroupMultiplier)
	require.Equal(t, 0.1, info.SubscriptionPackageMultiplier)
	require.Equal(t, 0.1, info.SubscriptionQuotaScale)
	require.NoError(t, session.Settle(1_000))
	require.Equal(t, int64(-900), settledDelta)
}

func TestMonthlyPassExpirySettlesAtFullPrice(t *testing.T) {
	previousHooks := subscriptionFundingHooks
	var settledDelta int64
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsume: func(_ string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
			return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 200, PreConsumed: amount, AmountTotal: 10_000, AmountUsedAfter: amount}, nil
		},
		PostConsumeDelta: func(_ int, _ string, delta int64) error {
			settledDelta = delta
			return nil
		},
		GetMonthlyPassEntitlement:      testMonthlyPassEntitlement,
		ValidateMonthlyPassEntitlement: func(int, MonthlyPassEntitlement) (bool, error) { return false, nil },
	})
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(previousHooks) })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 2, OriginModelName: "gpt-5", RequestId: "monthly-pass-expired-before-settlement",
		UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeOfficial},
		IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{FundingSourceOrder: []string{BillingSourceSubscription}},
		PriceData:   types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}

	session, apiErr := NewBillingSession(ctx, info, 1_000)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.NoError(t, session.Settle(1_000))
	require.Zero(t, settledDelta)
	require.Equal(t, 1.0, info.SubscriptionPackageMultiplier)
	require.Equal(t, 1.0, info.SubscriptionQuotaScale)
}

func TestMonthlyPassValidationErrorSettlesAtFullPrice(t *testing.T) {
	previousHooks := subscriptionFundingHooks
	var settledDelta int64
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsume: func(_ string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
			return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 201, PreConsumed: amount, AmountTotal: 10_000, AmountUsedAfter: amount}, nil
		},
		PostConsumeDelta: func(_ int, _ string, delta int64) error {
			settledDelta = delta
			return nil
		},
		GetMonthlyPassEntitlement: testMonthlyPassEntitlement,
		ValidateMonthlyPassEntitlement: func(int, MonthlyPassEntitlement) (bool, error) {
			return false, errors.New("entitlement database unavailable")
		},
	})
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(previousHooks) })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 2, OriginModelName: "gpt-5", RequestId: "monthly-pass-validation-error",
		UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeOfficial},
		IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{FundingSourceOrder: []string{BillingSourceSubscription}},
		PriceData:   types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}

	session, apiErr := NewBillingSession(ctx, info, 1_000)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.NoError(t, session.Settle(1_000))
	require.Zero(t, settledDelta)
	require.Equal(t, 1.0, info.SubscriptionPackageMultiplier)
}

func TestMarketplaceGroupUsesDerivedSubscriptionMultiplier(t *testing.T) {
	previousHooks := subscriptionFundingHooks
	var preConsumed int64
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsume: func(_ string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
			preConsumed = amount
			return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 299, PreConsumed: amount, AmountTotal: 10_000, AmountUsedAfter: amount}, nil
		},
	})
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(previousHooks) })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 3, OriginModelName: "gpt-5", RequestId: "marketplace-subscription-funded",
		UsingGroup: "marketplace-internal", MarketplaceGroupID: "marketplace-group",
		MarketplaceMultiplier: 0.06, ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeExternal},
		IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{FundingSourceOrder: []string{BillingSourceSubscription, BillingSourceWallet}},
		PriceData:   types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.06}},
	}

	session, apiErr := NewBillingSession(ctx, info, 60)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, int64(600), preConsumed)
	require.Equal(t, BillingSourceSubscription, info.BillingSource)
	require.InDelta(t, 0.6, info.SubscriptionGroupMultiplier, 1e-9)
	require.InDelta(t, 10, info.SubscriptionQuotaScale, 1e-9)
}

func TestMonthlyPassMultiplierDoesNotApplyAfterWalletFallback(t *testing.T) {
	setupMonthlyPassFundingTestDB(t)
	seedUser(t, 1102, 5_000)
	originalPolicy := gatewaystore.SubscriptionGroupPolicy2JSONString()
	require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString("{\"official\":{\"enabled\":true,\"multiplier\":1}}"))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(originalPolicy)) })

	previousHooks := subscriptionFundingHooks
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsume: func(string, int, string, int64) (*SubscriptionFundingPreConsumeResult, error) {
			return nil, errors.New("subscription quota insufficient")
		},
		GetMonthlyPassEntitlement:      testMonthlyPassEntitlement,
		ValidateMonthlyPassEntitlement: testValidMonthlyPassEntitlement,
	})
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(previousHooks) })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 1102, OriginModelName: "gpt-5", RequestId: "monthly-pass-wallet-fallback",
		UsingGroup: "official", ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeOfficial},
		IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{FundingSourceOrder: []string{BillingSourceSubscription, BillingSourceWallet}},
		PriceData:   types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}

	session, apiErr := NewBillingSession(ctx, info, 1_000)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, BillingSourceWallet, info.BillingSource)
	require.Equal(t, 1_000, session.GetPreConsumedQuota())
	quota, err := GetUserClaudeWalletQuota(info.UserId)
	require.NoError(t, err)
	require.Equal(t, 4_000, quota)
}

func TestMonthlyPassMultiplierDoesNotOverrideWalletFirst(t *testing.T) {
	setupMonthlyPassFundingTestDB(t)
	seedUser(t, 1103, 5_000)
	previousHooks := subscriptionFundingHooks
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsume: func(string, int, string, int64) (*SubscriptionFundingPreConsumeResult, error) {
			t.Fatal("wallet-first funding must not query subscription quota")
			return nil, nil
		},
		GetMonthlyPassEntitlement:      testMonthlyPassEntitlement,
		ValidateMonthlyPassEntitlement: testValidMonthlyPassEntitlement,
	})
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(previousHooks) })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 1103, OriginModelName: "gpt-5", RequestId: "monthly-pass-wallet-first",
		UsingGroup: "official", ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeOfficial},
		IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{FundingSourceOrder: []string{BillingSourceWallet, BillingSourceSubscription}},
	}

	session, apiErr := NewBillingSession(ctx, info, 1_000)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, BillingSourceWallet, info.BillingSource)
	require.Equal(t, 1_000, session.GetPreConsumedQuota())
}

func TestSubscriptionGroupPolicySkipsDisabledAndExternalGroups(t *testing.T) {
	originalPolicy := gatewaystore.SubscriptionGroupPolicy2JSONString()
	require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString("{\"disabled\":{\"enabled\":false,\"multiplier\":1},\"external\":{\"enabled\":true,\"multiplier\":1}}"))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(originalPolicy)) })

	previousHooks := subscriptionFundingHooks
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsume: func(string, int, string, int64) (*SubscriptionFundingPreConsumeResult, error) {
			t.Fatal("subscription funding must be skipped")
			return nil, nil
		},
	})
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(previousHooks) })

	tests := []struct {
		name  string
		group string
		scope string
	}{
		{name: "disabled official group", group: "disabled", scope: gatewayschema.ChannelScopeOfficial},
		{name: "external channel", group: "external", scope: gatewayschema.ChannelScopeExternal},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{
				UserId: 1010, OriginModelName: "gpt-5", RequestId: test.name,
				UsingGroup: test.group, ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: test.scope}, IsPlayground: true, ForcePreConsume: true,
				UserSetting: dto.UserSetting{FundingSourceOrder: []string{"subscription"}},
			}
			session, apiErr := NewBillingSession(ctx, info, 1_000+index)
			require.Nil(t, session)
			require.NotNil(t, apiErr)
			require.Contains(t, apiErr.Error(), "no available funding source")
		})
	}
}
