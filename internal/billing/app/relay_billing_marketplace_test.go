package app

import (
	"testing"

	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceSettlementParamsSeparateSubscriptionDebitAndGross(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RequestId: "request", MarketplaceGroupID: "group", MarketplaceOwnerID: 10,
		UserId: 20, BillingSource: BillingSourceSubscription,
		MarketplaceMultiplier: 0.06, SubscriptionGroupMultiplier: 0.6,
		SubscriptionPreConsumed: 600,
	}

	params := marketplaceSettlementParams(info, 60)
	require.Equal(t, int64(600), params.ConsumerDebitAmount)
	require.Equal(t, int64(60), params.SettlementGrossAmount)
	require.Equal(t, 0.06, params.WalletMultiplier)
	require.Equal(t, 0.6, params.SubscriptionMultiplier)
}
