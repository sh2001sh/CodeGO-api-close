package app

import (
	"encoding/json"
	"testing"

	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestOwnerUsageJSONFields(t *testing.T) {
	data, err := json.Marshal(OwnerUserUsageItem{RequestCount: 10, SuccessCount: 9, FailedCount: 1, AvgLatencyMs: 200, AvgTTFTMs: 100, TotalConsumerAmount: 600, TotalSettlementGrossAmount: 60, TotalOwnerIncome: 57})
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	for key, value := range map[string]float64{"request_count": 10, "success_count": 9, "failed_count": 1, "avg_latency_ms": 200, "avg_ttft_ms": 100, "total_consumer_amount": 600, "total_settlement_gross_amount": 60, "total_owner_income": 57} {
		require.Equal(t, value, fields[key], key)
	}
}

func TestOwnerUsageNormalizesAndAggregatesChannelUsers(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.Settlement{}, &identityschema.User{}))
	for _, g := range []marketplaceschema.Group{
		{ID: "g1", OwnerUserID: 10, ChannelID: "c1", PublicSlug: "g1", InternalGroupName: "g1"},
		{ID: "g3", OwnerUserID: 99, ChannelID: "private", PublicSlug: "g3", InternalGroupName: "g3"},
	} {
		require.NoError(t, db.Create(&g).Error)
	}
	for _, s := range []marketplaceschema.Settlement{
		{RequestID: "wallet-a", GroupID: "g1", OwnerUserID: 10, ConsumerUserID: 20, ConsumerAmount: 30, SettlementGrossAmount: 30, OwnerNetAmount: 28, BillingSource: "wallet"},
		{RequestID: "wallet-b", GroupID: "g1", OwnerUserID: 10, ConsumerUserID: 20, ConsumerAmount: 30, SettlementGrossAmount: 30, OwnerNetAmount: 28, BillingSource: "wallet"},
		{RequestID: "subscription", GroupID: "g1", OwnerUserID: 10, ConsumerUserID: 21, ConsumerAmount: 600, SettlementGrossAmount: 60, OwnerNetAmount: 57, BillingSource: "subscription", Multiplier: 0.06, SubscriptionMultiplier: 0.6},
		{RequestID: "private", GroupID: "g3", OwnerUserID: 99, ConsumerUserID: 22, ConsumerAmount: 9999, SettlementGrossAmount: 9999},
	} {
		require.NoError(t, db.Create(&s).Error)
	}
	result, err := ListOwnerChannelUserUsage(10, OwnerUserUsageQuery{ChannelID: "c1"})
	require.NoError(t, err)
	items := result["items"].([]OwnerUserUsageItem)
	require.Len(t, items, 2)
	byUser := map[string]OwnerUserUsageItem{}
	for _, item := range items {
		byUser[item.UserID] = item
	}
	require.Equal(t, int64(2), byUser["20"].RequestCount)
	require.Equal(t, int64(60), byUser["20"].TotalSettlementGrossAmount)
	require.Equal(t, int64(60), byUser["21"].TotalSettlementGrossAmount)
	require.Equal(t, int64(600), byUser["21"].TotalConsumerAmount)
	require.Equal(t, int64(3), result["summary"].(map[string]any)["total_requests"])
	empty, err := ListOwnerChannelUserUsage(10, OwnerUserUsageQuery{ChannelID: "private"})
	require.NoError(t, err)
	require.Empty(t, empty["items"])
}
