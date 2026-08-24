package app

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

type settledBillingStub struct {
	info    *relaycommon.RelayInfo
	pre     int
	charged int
}

func (stub *settledBillingStub) Settle(int) error {
	stub.info.BillingSettled = true
	stub.info.BillingSettledQuota = stub.charged
	return nil
}

func (*settledBillingStub) Refund(*gin.Context) {}
func (*settledBillingStub) NeedsRefund() bool   { return false }
func (stub *settledBillingStub) GetPreConsumedQuota() int {
	return stub.pre
}
func (*settledBillingStub) Reserve(int) error { return nil }

func TestMarketplaceSettlementParamsSeparateSubscriptionDebitAndGross(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RequestId: "request", MarketplaceGroupID: "group", MarketplaceOwnerID: 10,
		UserId: 20, BillingSource: BillingSourceSubscription,
		MarketplaceMultiplier: 0.06, SubscriptionGroupMultiplier: 0.6,
		SubscriptionPreConsumed: 600,
	}

	params := marketplaceSettlementParams(info, 600)
	require.Equal(t, int64(600), params.ConsumerDebitAmount)
	require.Equal(t, int64(60), params.SettlementGrossAmount)
	require.Equal(t, 0.06, params.WalletMultiplier)
	require.Equal(t, 0.6, params.SubscriptionMultiplier)
}

func TestMarketplaceSettlementParamsUseWalletDebitAsGross(t *testing.T) {
	info := &relaycommon.RelayInfo{BillingSource: BillingSourceWallet}
	params := marketplaceSettlementParams(info, 601)
	require.Equal(t, int64(601), params.ConsumerDebitAmount)
	require.Equal(t, int64(601), params.SettlementGrossAmount)
}

func TestMarketplaceSettlementParamsRoundSubscriptionGross(t *testing.T) {
	info := &relaycommon.RelayInfo{BillingSource: BillingSourceSubscription}
	require.Equal(t, int64(60), marketplaceSettlementParams(info, 604).SettlementGrossAmount)
	require.Equal(t, int64(61), marketplaceSettlementParams(info, 605).SettlementGrossAmount)
}

func TestSettleRelayBillingUsesFinalSubscriptionDebitEverywhere(t *testing.T) {
	truncate(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, platformdb.DB.Create(&identityschema.User{Id: 4091, Username: "settled-subscription"}).Error)
	info := &relaycommon.RelayInfo{
		RequestId: "settled-subscription-request", UserId: 4091, BillingSource: BillingSourceSubscription,
		MarketplaceGroupID: "market-group", MarketplaceOwnerID: 4092,
	}
	info.Billing = &settledBillingStub{info: info, pre: 500, charged: 600}

	require.NoError(t, SettleRelayBilling(ctx, info, 60))

	var user identityschema.User
	require.NoError(t, platformdb.DB.First(&user, 4091).Error)
	require.Equal(t, 600, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)

	var settlement marketplaceschema.Settlement
	require.NoError(t, platformdb.DB.First(&settlement, "request_id = ?", info.RequestId).Error)
	require.Equal(t, int64(600), settlement.ConsumerAmount)
	require.Equal(t, int64(60), settlement.SettlementGrossAmount)
	require.Equal(t, int64(57), settlement.OwnerNetAmount)
}
