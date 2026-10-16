package app

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	"github.com/sh2001sh/new-api/internal/billing/domain/billingexpr"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestSpeedTierActualWalletDebitAndAudit(t *testing.T) {
	oldExport := platformconfig.DataExportEnabled
	platformconfig.DataExportEnabled = false
	t.Cleanup(func() { platformconfig.DataExportEnabled = oldExport })
	userID := 1980
	for tier, multiplier := range map[string]int{"priority": 2, "ultrafast": 6, "": 1} {
		for _, pricing := range []string{"cached_tokens", "per_call", "empty_usage"} {
			t.Run(tier+"/"+pricing, func(t *testing.T) {
				truncate(t)
				userID++
				seedUser(t, userID, 100000)
				seedToken(t, userID+1000, userID, "speed-tier-test", 100000)
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				info := &relaycommon.RelayInfo{
					UserId: userID, TokenId: userID + 1000, TokenKey: "speed-tier-test", RequestId: "speed-" + tier + pricing,
					OriginModelName: "gpt-5", ServiceTier: tier, StartTime: time.Now(),
					IsPlayground: true, ForcePreConsume: true, ChannelMeta: &relaycommon.ChannelMeta{},
					UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
					PriceData:   types.PriceData{ModelRatio: 1, CompletionRatio: 2, CacheRatio: 0.1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1.5}},
				}
				usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 40}}
				base := 126 // (60 uncached + 40*0.1 cached + 10*2 output) * 1.5
				if pricing == "per_call" {
					info.PriceData.UsePrice = true
					info.PriceData.ModelPrice = 0.01
					base = 7500
				}
				if pricing == "empty_usage" {
					usage = &dto.Usage{}
					base = 0
				}
				require.Nil(t, PreConsumeRelayBilling(ctx, 1000, info))
				require.EqualValues(t, 100000-1000*multiplier, loadBillingSnapshot(t, userID, "claude_wallet").AvailableBalance)
				require.Nil(t, PreConsumeRelayBilling(ctx, 1000, info), "retry reuses one reservation")
				require.EqualValues(t, 100000-1000*multiplier, loadBillingSnapshot(t, userID, "claude_wallet").AvailableBalance)
				PostTextConsumeQuota(ctx, info, usage, nil)
				snapshot := loadBillingSnapshot(t, userID, "claude_wallet")
				require.EqualValues(t, 100000-base*multiplier, snapshot.AvailableBalance)
				require.EqualValues(t, base*multiplier, snapshot.ConsumedTotal)
				var log auditschema.Log
				require.NoError(t, platformdb.DB.Where("user_id = ?", userID).Last(&log).Error)
				require.Equal(t, base*multiplier, log.Quota)
				var other map[string]any
				require.NoError(t, json.Unmarshal([]byte(log.Other), &other))
				require.EqualValues(t, multiplier, other["service_tier_multiplier"])
				require.EqualValues(t, base, other["quota_before_service_tier"])
				require.Contains(t, other["billing_calculation"], fmt.Sprintf("%d \u00d7 %d", base, multiplier))
			})
		}
	}
}

func TestSpeedTierFailedRequestRefundsFullReservation(t *testing.T) {
	for index, tier := range []string{"priority", "ultrafast"} {
		t.Run(tier, func(t *testing.T) {
			userID := 1995 + index
			truncate(t)
			seedUser(t, userID, 10000)
			seedToken(t, userID+1000, userID, "speed-refund", 10000)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{UserId: userID, TokenId: userID + 1000, TokenKey: "speed-refund", RequestId: "refund-" + tier, OriginModelName: "gpt-5", ServiceTier: tier, IsPlayground: true, ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
			require.Nil(t, PreConsumeRelayBilling(ctx, 1000, info))
			require.NoError(t, RefundRelayBillingSync(ctx, info))
			require.NoError(t, RefundRelayBillingSync(ctx, info))
			require.EqualValues(t, 10000, loadBillingSnapshot(t, userID, "claude_wallet").AvailableBalance)
		})
	}
}

func TestSpeedTierTieredFallbackDoesNotMultiplyTwice(t *testing.T) {
	for _, tier := range []string{"priority", "ultrafast"} {
		info := &relaycommon.RelayInfo{ServiceTier: tier, TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: "invalid(", EstimatedQuotaAfterGroup: 1000}}
		info.FinalPreConsumedQuota = info.ServiceTierQuota(1000)
		ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{})
		require.True(t, ok)
		require.Nil(t, result)
		require.Equal(t, info.FinalPreConsumedQuota, info.ServiceTierQuota(quota))
	}
}

func TestSpeedTierSubscriptionConversionOnce(t *testing.T) {
	oldPolicy := gatewaystore.SubscriptionGroupPolicy2JSONString()
	require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(`{"official":{"enabled":true,"multiplier":0.5}}`))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(oldPolicy)) })
	oldHooks := subscriptionFundingHooks
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(oldHooks) })
	for _, tier := range []string{"priority", "ultrafast"} {
		t.Run(tier, func(t *testing.T) {
			var reserved, delta int64
			RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
				PreConsume: func(_ string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
					reserved = amount
					return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 99, PreConsumed: amount, AmountTotal: 100000, AmountUsedAfter: amount}, nil
				},
				PostConsumeDelta: func(_ int, _ string, amount int64) error { delta = amount; return nil },
			})
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{UserId: 1970, OriginModelName: "gpt-5", RequestId: "tier-plan-" + tier, ServiceTier: tier, UsingGroup: "official", IsPlayground: true, ForcePreConsume: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeOfficial}, PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}}, UserSetting: dto.UserSetting{BillingPreference: "subscription_only"}}
			require.Nil(t, PreConsumeRelayBilling(ctx, 1000, info))
			multiplier := relaycommon.ServiceTierMultiplier(tier)
			require.EqualValues(t, 250*multiplier, reserved)
			require.NoError(t, info.Billing.Settle(1600*multiplier))
			require.EqualValues(t, 150*multiplier, delta)
			require.Equal(t, 400*multiplier, BillingQuotaForLog(info, 1600*multiplier))
		})
	}
}
