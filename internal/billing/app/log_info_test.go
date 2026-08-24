package app

import (
	"net/http/httptest"
	"testing"
	"time"

	gatewaysruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoIncludesFirstByteTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	startedAt := time.Now().Add(-time.Second)
	relayInfo := &gatewaysruntime.RelayInfo{
		StartTime:         startedAt,
		FirstResponseTime: startedAt.Add(900 * time.Millisecond),
		FirstByteTrace:    gatewaysruntime.NewFirstByteTrace(startedAt),
		StreamPacer:       gatewaysruntime.NewStreamPacer("gpt-5.6-sol"),
		ChannelMeta:       &gatewaysruntime.ChannelMeta{},
	}
	relayInfo.FirstByteTrace.MarkRelayInfoReady()
	relayInfo.FirstByteTrace.MarkPreflightDone()
	relayInfo.FirstByteTrace.MarkRouteSelected()
	relayInfo.FirstByteTrace.MarkUpstreamStart()
	relayInfo.FirstByteTrace.MarkFirstEvent()
	relayInfo.FirstByteTrace.MarkFirstSemanticEvent()

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	require.Contains(t, other, "first_byte_trace")
	require.Contains(t, other, "response_start_ms")
	require.Greater(t, other["response_start_ms"].(int64), int64(0))
	trace, ok := other["first_byte_trace"].(map[string]int64)
	require.True(t, ok)
	require.Greater(t, trace["total_ms"], int64(0))
}

func TestAppendBillingInfoIncludesQuotaSource(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		category string
		label    string
	}{
		{name: "universal quota", source: BillingSourceWallet, category: "universal", label: "通用额度"},
		{name: "GPT plan quota", source: BillingSourceSubscription, category: "subscription", label: "GPT 套餐额度"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := make(map[string]interface{})
			appendBillingInfo(&gatewaysruntime.RelayInfo{BillingSource: tt.source}, other)

			require.Equal(t, tt.source, other["billing_source"])
			require.Equal(t, tt.category, other["billing_quota_category"])
			require.Equal(t, tt.label, other["billing_quota_label"])
		})
	}
}

func TestAppendBillingInfoMarketplaceUsesSelectedFundingSource(t *testing.T) {
	other := make(map[string]interface{})
	appendBillingInfo(&gatewaysruntime.RelayInfo{
		BillingSource:      BillingSourceSubscription,
		MarketplaceGroupID: "Codex-Plus-abc123",
	}, other)

	require.Equal(t, "subscription", other["billing_quota_category"])
	require.Equal(t, "GPT 套餐额度", other["billing_quota_label"])
	require.Equal(t, 0.95, other["marketplace_owner_net_rate"])
}

func TestBillingQuotaForLogUsesMonthlyPassSettlement(t *testing.T) {
	info := &gatewaysruntime.RelayInfo{
		BillingSource:           BillingSourceSubscription,
		SubscriptionPreConsumed: 10_000,
		SubscriptionPostDelta:   2_500,
	}
	require.Equal(t, 12_500, BillingQuotaForLog(info, 2_500))

	info.BillingSource = BillingSourceWallet
	require.Equal(t, 2_500, BillingQuotaForLog(info, 2_500))
}

func TestAppendBillingInfoIncludesMonthlyPassPolicy(t *testing.T) {
	other := make(map[string]interface{})
	appendBillingInfo(&gatewaysruntime.RelayInfo{
		BillingSource:               BillingSourceSubscription,
		SubscriptionGroupMultiplier: 1.5,
		SubscriptionPackageMultiplier: 0.9,
		SubscriptionQuotaScale:      15,
		SubscriptionGroupRatio:      0.1,
	}, other)

	require.Equal(t, 1.5, other["subscription_group_multiplier"])
	require.Equal(t, 0.9, other["subscription_package_multiplier"])
	require.Equal(t, float64(15), other["subscription_quota_scale"])
	require.Equal(t, 0.1, other["subscription_group_ratio"])
}
