package app

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	"github.com/stretchr/testify/require"
)

func TestCreditPackPricingAndMarketplaceIsolation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		legacy bool
		scope  string
		market string
		policy string
		accept bool
		want   int64
	}{
		{"legacy_keeps_tenfold", true, gatewayschema.ChannelScopeExternal, "market-1", "", false, 1000},
		{"new_market_onefold", false, gatewayschema.ChannelScopeExternal, "market-1", "", true, 100},
		{"universal_market_accepts_new_only", false, gatewayschema.ChannelScopeExternal, "market-1", marketplacedomain.CreditPolicyUniversalOnly, true, 100},
		{"official_defaults_to_new", false, gatewayschema.ChannelScopeOfficial, "", "", false, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := subscriptionFundingHooks
			t.Cleanup(func() { subscriptionFundingHooks = old })
			var reserved, settled int64
			RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
				PreConsume: func(_ string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
					if !tc.legacy {
						return nil, errors.New("no active subscription")
					}
					reserved = amount
					return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 1, PreConsumed: amount, ReservationID: "legacy", AmountTotal: 10000}, nil
				},
				PreConsumeCreditPack: func(_ string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
					reserved = amount
					return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 2, PreConsumed: amount, ReservationID: "pack", AmountTotal: 10000,
						CreditPackGrantedQuota: 11000, CreditPackPaidQuota: 10000, CreditPackChannelShareBPS: 6000}, nil
				},
				PostConsumeDelta: func(_ int, _ string, delta int64) error { settled = delta; return nil },
			})
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{UserId: 1, RequestId: tc.name, IsPlayground: true, ForcePreConsume: true,
				MarketplaceGroupID: tc.market, MarketplaceMultiplier: 1, MarketplaceCreditPolicy: tc.policy,
				UserSetting: dto.UserSetting{BillingPreference: "subscription_first"},
				ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: tc.scope, CreditPackSupported: tc.accept},
			}
			session, apiErr := NewBillingSession(ctx, info, 100)
			require.Nil(t, apiErr)
			require.NotNil(t, session)
			require.Equal(t, tc.want, reserved)
			// A pricing-only mock cannot atomically commit the native debit and
			// marketplace intent. Real reservations are covered by integration tests.
			require.EqualValues(t, tc.want*4/5, session.scaledSettlementQuota(80))
			require.ErrorContains(t, session.Settle(80), "transactional subscription settlement hook is not registered")
			require.False(t, info.BillingSettled)
			require.Zero(t, settled)
			params := marketplaceSettlementParams(info, int(tc.want*4/5))
			require.Equal(t, int64(80), params.SettlementGrossAmount)
			if !tc.legacy {
				require.Equal(t, int64(6000), params.CreditPackChannelShareBPS)
				require.Equal(t, int64(11000), params.CreditPackGrantedQuota)
			}
		})
	}
}

func TestCreditPackRejectsUnenrolledChannelsAndRetry(t *testing.T) {
	old := subscriptionFundingHooks
	t.Cleanup(func() { subscriptionFundingHooks = old })
	called := false
	subscriptionFundingHooks.PreConsumeCreditPack = func(string, int, string, int64) (*SubscriptionFundingPreConsumeResult, error) {
		called = true
		return nil, errors.New("unexpected reservation")
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{MarketplaceGroupID: "market-1", ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeExternal}}
	session, apiErr := newCreditPackBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.Nil(t, session)
	require.False(t, called)
	info.CreditPackGrantedQuota = 110
	info.Billing = &BillingSession{}
	require.NotNil(t, PreConsumeRelayBilling(ctx, 100, info))
}

func TestCreditPackMissingSnapshotRefundsReservation(t *testing.T) {
	old := subscriptionFundingHooks
	t.Cleanup(func() { subscriptionFundingHooks = old })
	refunded := false
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsumeCreditPack: func(string, int, string, int64) (*SubscriptionFundingPreConsumeResult, error) {
			return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 1, PreConsumed: 100, ReservationID: "pack"}, nil
		},
		RefundPreConsume: func(string) error { refunded = true; return nil },
	})
	funding := &SubscriptionFunding{requestID: "missing-snapshot", creditPack: true}
	require.ErrorContains(t, funding.PreConsume(100), "invalid credit pack funding snapshot")
	require.True(t, refunded)
}

func TestCreditPackDoesNotConsumeDiscountCards(t *testing.T) {
	old := subscriptionFundingHooks
	t.Cleanup(func() { subscriptionFundingHooks = old })
	called := false
	subscriptionFundingHooks.ApplyBlindBoxConsumptionDiscount = func(BlindBoxConsumptionDiscountRequest) (BlindBoxConsumptionDiscountResult, error) {
		called = true
		return BlindBoxConsumptionDiscountResult{}, nil
	}
	info := &relaycommon.RelayInfo{CreditPackGrantedQuota: 110, ChannelMeta: &relaycommon.ChannelMeta{MultiplierCardUserEnabled: true}}
	require.Equal(t, 100, applyUsageConsumptionDiscount(info, 100))
	require.False(t, called)
}

func TestCreditPackLimitedKeyCanFallBackFromTenfoldLegacyAttempt(t *testing.T) {
	truncate(t)
	seedUser(t, 1801, 0)
	seedToken(t, 2801, 1801, "sk-credit-pack-limited", 200)
	old := subscriptionFundingHooks
	t.Cleanup(func() { subscriptionFundingHooks = old })
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsume: func(string, int, string, int64) (*SubscriptionFundingPreConsumeResult, error) {
			return nil, errors.New("no active subscription")
		},
		PreConsumeCreditPack: func(_ string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
			return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 2, PreConsumed: amount, ReservationID: "pack", AmountTotal: 11000,
				CreditPackGrantedQuota: 11000, CreditPackPaidQuota: 10000, CreditPackChannelShareBPS: 6000}, nil
		},
		RefundPreConsume: func(string) error { return nil },
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 1801, TokenId: 2801, TokenKey: "sk-credit-pack-limited", RequestId: "limited-key-pack",
		MarketplaceGroupID: "market-1", MarketplaceMultiplier: 1, ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "subscription_only"},
		ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeExternal, CreditPackSupported: true},
	}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, 100, session.GetPreConsumedQuota())
	require.NoError(t, session.RefundSync(ctx))
}
