package app

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

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
