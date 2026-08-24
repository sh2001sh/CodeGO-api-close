package app

import (
	"testing"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestApplyBlindBoxConsumptionDiscountIsOfficialUnlimitedAndIdempotent(t *testing.T) {
	db := setupRedemptionTestDB(t)
	prop := &commerceschema.BlindBoxProp{
		UserId: 7001, PropType: commerceschema.BlindBoxPropTypeConsumeDiscount90,
		Title: "0.9 倍率卡", Status: commerceschema.BlindBoxPropStatusActive,
		DiscountRate: 0.1, Multiplier: 0.9,
		ExpiresAt: platformruntime.GetTimestamp() + 3600,
	}
	require.NoError(t, db.Create(prop).Error)

	request := billingapp.BlindBoxConsumptionDiscountRequest{
		RequestID: "discount-official-1", UserID: prop.UserId, ChannelID: 10,
		ChannelScope: gatewayschema.ChannelScopeOfficial, ModelName: "gpt-5", Quota: 1000,
	}
	first, err := ApplyBlindBoxConsumptionDiscount(request)
	require.NoError(t, err)
	require.Equal(t, 900, first.QuotaAfterDiscount)
	require.Equal(t, 100, first.DiscountQuota)
	require.Equal(t, 0.9, first.NominalMultiplier)
	require.Equal(t, 0.9, first.EffectiveMultiplier)
	require.Zero(t, first.RemainingDiscountQuota)

	replayed, err := ApplyBlindBoxConsumptionDiscount(request)
	require.NoError(t, err)
	require.Equal(t, first, replayed)

	request.RequestID = "discount-official-2"
	second, err := ApplyBlindBoxConsumptionDiscount(request)
	require.NoError(t, err)
	require.Equal(t, 900, second.QuotaAfterDiscount)
	require.Equal(t, 100, second.DiscountQuota)
	require.Zero(t, second.RemainingDiscountQuota)

	require.NoError(t, db.First(prop, prop.Id).Error)
	require.Equal(t, int64(200), prop.UsedDiscountQuota)
	require.Equal(t, commerceschema.BlindBoxPropStatusActive, prop.Status)
	var usageCount int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxPropDiscountUsage{}).Count(&usageCount).Error)
	require.Equal(t, int64(2), usageCount)
}

func TestApplyBlindBoxConsumptionDiscountSeparatesNominalAndEffectiveMultiplier(t *testing.T) {
	db := setupRedemptionTestDB(t)
	prop := &commerceschema.BlindBoxProp{
		UserId: 7005, PropType: commerceschema.BlindBoxPropTypeConsumeDiscount90,
		Title: "0.9 倍率卡", Status: commerceschema.BlindBoxPropStatusActive,
		DiscountRate: 0.1, Multiplier: 0.9,
		ExpiresAt: platformruntime.GetTimestamp() + 3600,
	}
	require.NoError(t, db.Create(prop).Error)

	result, err := ApplyBlindBoxConsumptionDiscount(billingapp.BlindBoxConsumptionDiscountRequest{
		RequestID: "discount-effective-ratio", UserID: prop.UserId, ChannelID: 10,
		ChannelScope: gatewayschema.ChannelScopeOfficial, ModelName: "gpt-5", Quota: 999,
	})
	require.NoError(t, err)
	require.Equal(t, 899, result.QuotaAfterDiscount)
	require.Equal(t, 0.9, result.NominalMultiplier)
	require.Equal(t, 0.8999, result.EffectiveMultiplier)

	var usage commerceschema.BlindBoxPropDiscountUsage
	require.NoError(t, db.Where("request_id = ?", "discount-effective-ratio").First(&usage).Error)
	require.Equal(t, 0.9, usage.Multiplier)
	require.Equal(t, 0.8999, usage.EffectiveMultiplier)
}

func TestApplyBlindBoxConsumptionDiscountSkipsExternalChannel(t *testing.T) {
	db := setupRedemptionTestDB(t)
	prop := &commerceschema.BlindBoxProp{
		UserId: 7002, PropType: commerceschema.BlindBoxPropTypeConsumeDiscount95,
		Title: "0.95 倍率卡", Status: commerceschema.BlindBoxPropStatusActive,
		DiscountRate: 0.05, Multiplier: 0.95,
		ExpiresAt: platformruntime.GetTimestamp() + 3600,
	}
	require.NoError(t, db.Create(prop).Error)

	result, err := ApplyBlindBoxConsumptionDiscount(billingapp.BlindBoxConsumptionDiscountRequest{
		RequestID: "discount-external", UserID: prop.UserId, ChannelID: 20,
		ChannelScope: gatewayschema.ChannelScopeExternal, ModelName: "gpt-5", Quota: 1000,
	})
	require.NoError(t, err)
	require.Equal(t, 1000, result.QuotaAfterDiscount)
	require.Zero(t, result.DiscountQuota)

	require.NoError(t, db.First(prop, prop.Id).Error)
	require.Zero(t, prop.UsedDiscountQuota)
}

func TestApplyBlindBoxConsumptionDiscountIgnoresPackageMultiplier(t *testing.T) {
	db := setupRedemptionTestDB(t)
	prop := &commerceschema.BlindBoxProp{
		UserId: 7003, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		Title: "0.10 倍率体验卡", Status: commerceschema.BlindBoxPropStatusActive,
		DiscountRate: 0.9, Multiplier: 0.1,
		ExpiresAt: platformruntime.GetTimestamp() + 900,
	}
	require.NoError(t, db.Create(prop).Error)

	result, err := ApplyBlindBoxConsumptionDiscount(billingapp.BlindBoxConsumptionDiscountRequest{
		RequestID: "discount-monthly-pass", UserID: prop.UserId, ChannelID: 30,
		ChannelScope: gatewayschema.ChannelScopeOfficial, ModelName: "gpt-5",
		UsingGroup: "official", Quota: 1000,
	})
	require.NoError(t, err)
	require.Equal(t, 1000, result.QuotaBeforeDiscount)
	require.Equal(t, 1000, result.QuotaAfterDiscount)
	require.Zero(t, result.DiscountQuota)
}

func TestApplyBlindBoxUniversalMultiplierUsesSelectedOfficialGroup(t *testing.T) {
	db := setupRedemptionTestDB(t)
	prop := &commerceschema.BlindBoxProp{
		UserId: 7004, PropType: commerceschema.BlindBoxPropTypeConsumeDiscount10,
		Title: "0.1 倍率卡", Status: commerceschema.BlindBoxPropStatusActive,
		DiscountRate: 0.9, Multiplier: 0.1,
		ExpiresAt: platformruntime.GetTimestamp() + 900,
	}
	require.NoError(t, db.Create(prop).Error)

	result, err := ApplyBlindBoxConsumptionDiscount(billingapp.BlindBoxConsumptionDiscountRequest{
		RequestID: "discount-blind-box-universal", UserID: prop.UserId, ChannelID: 31,
		ChannelScope: gatewayschema.ChannelScopeOfficial, ModelName: "claude-sonnet-4",
		UsingGroup: "official-claude", Quota: 1000,
	})
	require.NoError(t, err)
	require.Equal(t, 1000, result.QuotaBeforeDiscount)
	require.Equal(t, 100, result.QuotaAfterDiscount)
	require.Equal(t, 900, result.DiscountQuota)
}
