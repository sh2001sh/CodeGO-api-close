package app

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestRealtimeChunksUseOneReservationAndFinalSettlement(t *testing.T) {
	truncate(t)
	seedUser(t, 11801, 1000)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 11801, RequestId: "realtime-cumulative", IsPlayground: true,
		ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.NoError(t, session.reserveRealtimeUsage(60))
	require.Equal(t, 100, session.GetPreConsumedQuota())
	require.NoError(t, session.reserveRealtimeUsage(60))
	require.Equal(t, 120, session.GetPreConsumedQuota())
	snapshot := loadBillingSnapshot(t, info.UserId, billingAccountTypeClaudeWallet)
	require.EqualValues(t, 880, snapshot.AvailableBalance)
	require.EqualValues(t, 120, snapshot.ReservedBalance)
	require.Zero(t, snapshot.ConsumedTotal)
	require.NoError(t, session.Settle(120))
	require.NoError(t, session.Settle(120))
	snapshot = loadBillingSnapshot(t, info.UserId, billingAccountTypeClaudeWallet)
	require.EqualValues(t, 880, snapshot.AvailableBalance)
	require.Zero(t, snapshot.ReservedBalance)
	require.EqualValues(t, 120, snapshot.ConsumedTotal)
	var count int64
	require.NoError(t, platformdb.DB.Model(&billingschema.BillingSettlement{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestRealtimeReserveFailureRestoresTokenAndPreservesCumulativeUsage(t *testing.T) {
	truncate(t)
	seedUser(t, 11802, 100)
	seedToken(t, 11803, 11802, "realtime-failure-token", 1000)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 11802, TokenId: 11803, TokenKey: "realtime-failure-token",
		RequestId: "realtime-failure", ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := NewBillingSession(ctx, info, 60)
	require.Nil(t, apiErr)
	require.NoError(t, session.reserveRealtimeUsage(60))
	require.Error(t, session.reserveRealtimeUsage(60))
	require.Equal(t, 60, session.realtimeUsageQuota)
	require.Equal(t, 60, session.GetPreConsumedQuota())
	token, err := GetTokenByID(info.TokenId)
	require.NoError(t, err)
	require.Equal(t, 940, token.RemainQuota)
	snapshot := loadBillingSnapshot(t, info.UserId, billingAccountTypeClaudeWallet)
	require.EqualValues(t, 40, snapshot.AvailableBalance)
	require.EqualValues(t, 60, snapshot.ReservedBalance)
	require.NoError(t, session.reserveRealtimeUsage(40))
	require.Equal(t, 100, session.GetPreConsumedQuota())
	require.Error(t, session.reserveRealtimeUsage(-1))
	require.Error(t, session.reserveRealtimeUsage(math.MaxInt))
	require.NoError(t, session.RefundSync(ctx))
	snapshot = loadBillingSnapshot(t, info.UserId, billingAccountTypeClaudeWallet)
	require.EqualValues(t, 100, snapshot.AvailableBalance)
}

func TestRealtimeSubscriptionUsesFundingHoldWithoutWalletAndAppliesServiceTier(t *testing.T) {
	oldHooks := subscriptionFundingHooks
	t.Cleanup(func() { subscriptionFundingHooks = oldHooks })
	var additional int64
	subscriptionFundingHooks = SubscriptionFundingHooks{ReserveAdditional: func(_ string, _ int, _ string, amount int64) error {
		additional += amount
		return nil
	}, ApplyBlindBoxConsumptionDiscount: func(BlindBoxConsumptionDiscountRequest) (BlindBoxConsumptionDiscountResult, error) {
		t.Fatal("provisional realtime holds must not consume a discount card")
		return BlindBoxConsumptionDiscountResult{}, nil
	}}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 11804, OriginModelName: "gpt-5", UsingGroup: "default", ServiceTier: "priority", IsPlayground: true,
		ChannelMeta: &relaycommon.ChannelMeta{}}
	funding := &SubscriptionFunding{requestID: "realtime-subscription", subscriptionID: 11804, preConsumed: 100}
	session := &BillingSession{relayInfo: info, funding: funding, preConsumedQuota: 100, reservationQuotaScale: 10}
	info.Billing = session
	usage := &dto.RealtimeUsage{InputTokenDetails: dto.InputTokenDetails{TextTokens: 60}}
	ratio, _, _ := gatewaystore.GetModelRatio(info.OriginModelName)
	base := calculateAudioQuota(QuotaInfo{InputDetails: TokenDetails{TextTokens: 60}, ModelName: info.OriginModelName,
		ModelRatio: ratio, GroupRatio: gatewaystore.GetGroupRatio(info.UsingGroup)})
	require.Positive(t, base)
	require.NoError(t, PreWssConsumeQuota(ctx, info, usage))
	require.NoError(t, PreWssConsumeQuota(ctx, info, usage))
	require.EqualValues(t, base*2*2*10-100, additional)
	require.Equal(t, base*2*2*10, session.GetPreConsumedQuota())
	// No user or token exists: this path can only succeed through subscription
	// reservation, never an unrelated wallet balance or an immediate debit.
	require.Zero(t, info.SubscriptionPostDelta)
}
