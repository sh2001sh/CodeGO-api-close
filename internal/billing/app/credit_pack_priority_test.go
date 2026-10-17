package app

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func creditPackPriorityContext(t *testing.T, pref string, wallet int) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	truncate(t)
	user := identityschema.User{Username: "priority-" + t.Name(), ClaudeQuota: wallet, Status: constant.UserStatusEnabled}
	require.NoError(t, platformdb.DB.Create(&user).Error)
	previousHooks := subscriptionFundingHooks
	previousPolicy := gatewaystore.SubscriptionGroupPolicy2JSONString()
	require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(`{"official":{"enabled":true,"multiplier":0.5}}`))
	t.Cleanup(func() {
		RegisterSubscriptionFundingHooks(previousHooks)
		require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(previousPolicy))
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	return ctx, &relaycommon.RelayInfo{
		UserId: user.Id, RequestId: "priority-" + t.Name(), OriginModelName: "gpt-5", UsingGroup: "official",
		IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: pref},
		ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeOfficial},
		PriceData:   types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}},
	}
}

func TestCreditPackPriorityAndPolicyFallback(t *testing.T) {
	for _, test := range []struct {
		name, pref, policy, want       string
		wallet, packCalls, legacyCalls int
		external, supported            bool
		packErr, legacyErr             error
	}{
		{name: "both_available_new_pack_first", pref: "subscription_first", wallet: 10000, want: "pack", packCalls: 1},
		{name: "pack_insufficient_legacy", pref: "subscription_first", wallet: 10000, packErr: billingdomain.ErrInsufficientBalance, want: "legacy", packCalls: 1, legacyCalls: 1},
		{name: "pack_missing_legacy_only", pref: "subscription_only", wallet: 10000, packErr: errors.New("no active subscription"), want: "legacy", packCalls: 1, legacyCalls: 1},
		{name: "wallet_first_unchanged", pref: "wallet_first", wallet: 10000, want: "wallet"},
		{name: "wallet_insufficient_then_pack", pref: "wallet_first", want: "pack", packCalls: 1},
		{name: "disabled_pack_legacy", pref: "subscription_first", external: true, wallet: 10000, want: "legacy", legacyCalls: 1},
		{name: "disabled_pack_legacy_missing_wallet", pref: "subscription_first", external: true, wallet: 10000, legacyErr: errors.New("no active subscription"), want: "wallet", legacyCalls: 1},
		{name: "disabled_pack_subscription_only_no_wallet", pref: "subscription_only", external: true, wallet: 10000, legacyErr: errors.New("no active subscription"), legacyCalls: 1},
		{name: "disabled_pack_universal_wallet", pref: "subscription_first", external: true, policy: marketplacedomain.CreditPolicyUniversalOnly, wallet: 10000, want: "wallet"},
		{name: "universal_never_legacy", pref: "subscription_first", external: true, supported: true, policy: marketplacedomain.CreditPolicyUniversalOnly, wallet: 10000, packErr: billingdomain.ErrInsufficientBalance, want: "wallet", packCalls: 1},
		{name: "both_insufficient_wallet", pref: "subscription_first", wallet: 10000, packErr: billingdomain.ErrInsufficientBalance, legacyErr: billingdomain.ErrInsufficientBalance, want: "wallet", packCalls: 1, legacyCalls: 1},
		{name: "only_both_insufficient_no_wallet", pref: "subscription_only", wallet: 10000, packErr: billingdomain.ErrInsufficientBalance, legacyErr: billingdomain.ErrInsufficientBalance, packCalls: 1, legacyCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, info := creditPackPriorityContext(t, test.pref, test.wallet)
			if test.external {
				info.ChannelScope, info.ChannelMeta.CreditPackSupported = gatewayschema.ChannelScopeExternal, test.supported
				info.MarketplaceGroupID, info.MarketplaceMultiplier = "market", 1
			}
			info.MarketplaceCreditPolicy = test.policy
			info.ChannelMeta.MultiplierCardUserEnabled = test.want == "pack"
			var packCalls, legacyCalls, refunds int
			RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
				PreConsumeCreditPack: func(request string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
					packCalls++
					require.True(t, strings.HasPrefix(request, "cp:"))
					require.Len(t, request, 63)
					require.EqualValues(t, 100, amount)
					if test.packErr != nil {
						return nil, test.packErr
					}
					return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 22, PreConsumed: amount, ReservationID: "pack", PlanID: 2, PlanTitle: "New pack", AmountTotal: 103000, CreditPackGrantedQuota: 103000, CreditPackPaidQuota: 100000, CreditPackChannelShareBPS: 6000}, nil
				},
				PreConsume: func(request string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
					legacyCalls++
					require.Equal(t, info.RequestId, request)
					if test.external {
						require.EqualValues(t, 500, amount)
					} else {
						require.EqualValues(t, 25, amount)
					}
					if test.legacyErr != nil {
						return nil, test.legacyErr
					}
					return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 11, PreConsumed: amount, ReservationID: "legacy", PlanID: 1, PlanTitle: "Legacy pass", AmountTotal: 10000}, nil
				},
				RefundPreConsume: func(string) error { refunds++; return nil },
				GetMonthlyPassEntitlement: func(int) (*MonthlyPassEntitlement, error) {
					return nil, errors.New("new credit pack must not query legacy discount cards")
				},
			})
			session, apiErr := NewBillingSession(ctx, info, 100)
			require.Equal(t, test.packCalls, packCalls)
			require.Equal(t, test.legacyCalls, legacyCalls)
			if test.want == "" {
				require.Nil(t, session)
				require.NotNil(t, apiErr)
				require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
				require.Zero(t, info.SubscriptionQuotaScale)
				return
			}
			require.Nil(t, apiErr)
			require.NotNil(t, session)
			switch test.want {
			case "pack":
				require.True(t, session.funding.(*SubscriptionFunding).creditPack)
				require.Equal(t, 22, info.SubscriptionId)
				require.Equal(t, "New pack", info.SubscriptionPlanTitle)
				require.Equal(t, 1.0, info.SubscriptionQuotaScale)
				require.Equal(t, 100, session.scaledSettlementQuota(100))
			case "legacy":
				require.False(t, session.funding.(*SubscriptionFunding).creditPack)
				require.Equal(t, 11, info.SubscriptionId)
				require.Equal(t, "Legacy pass", info.SubscriptionPlanTitle)
				require.Zero(t, info.CreditPackGrantedQuota)
				require.Equal(t, session.GetPreConsumedQuota(), session.scaledSettlementQuota(100))
			case "wallet":
				require.Equal(t, BillingSourceWallet, info.BillingSource)
				require.Zero(t, info.SubscriptionId)
				require.Zero(t, info.SubscriptionQuotaScale)
				require.Zero(t, info.CreditPackGrantedQuota)
			}
			info.Billing = session
			require.Nil(t, PreConsumeRelayBilling(ctx, 100, info))
			require.Equal(t, test.packCalls, packCalls, "retry must retain its funding source")
			require.Equal(t, test.legacyCalls, legacyCalls)
			require.NoError(t, session.RefundSync(ctx))
			require.NoError(t, session.RefundSync(ctx))
			if test.want != "wallet" {
				require.Equal(t, 1, refunds)
			}
		})
	}
}

func TestCreditPackPriorityStopsOnNonBalanceErrors(t *testing.T) {
	for _, test := range []struct {
		name                                                 string
		invalidSnapshot, refundFailure, parallelTokenFailure bool
	}{
		{name: "backend_failure"},
		{name: "invalid_snapshot", invalidSnapshot: true},
		{name: "invalid_snapshot_refund_failure", invalidSnapshot: true, refundFailure: true},
		{name: "parallel_token_and_backend_failure", parallelTokenFailure: true},
		{name: "parallel_token_and_refund_failure", invalidSnapshot: true, refundFailure: true, parallelTokenFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, info := creditPackPriorityContext(t, "subscription_first", 10000)
			if test.parallelTokenFailure {
				seedToken(t, info.UserId+1000000, info.UserId, "priority-error", 50)
				info.IsPlayground, info.TokenId, info.TokenKey = false, info.UserId+1000000, "priority-error"
				previousSQLite := platformdb.UsingSQLite
				platformdb.UsingSQLite = false
				t.Cleanup(func() { platformdb.UsingSQLite = previousSQLite })
			}
			failure := errors.New("injected pack database failure")
			refundFailure := errors.New("injected pack refund failure")
			legacyCalls := 0
			RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
				PreConsumeCreditPack: func(_ string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
					if !test.invalidSnapshot {
						return nil, failure
					}
					return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 22, PreConsumed: amount, ReservationID: "pack", PlanID: 2}, nil
				},
				PreConsume: func(string, int, string, int64) (*SubscriptionFundingPreConsumeResult, error) {
					legacyCalls++
					return nil, errors.New("unexpected legacy attempt")
				},
				RefundPreConsume: func(string) error {
					if test.refundFailure {
						return refundFailure
					}
					return nil
				},
			})
			session, apiErr := NewBillingSession(ctx, info, 100)
			require.Nil(t, session)
			require.NotNil(t, apiErr)
			require.Equal(t, types.ErrorCodeUpdateDataError, apiErr.GetErrorCode())
			if !test.invalidSnapshot {
				require.ErrorIs(t, apiErr, failure)
			}
			if test.refundFailure {
				require.ErrorIs(t, apiErr, refundFailure)
			}
			require.Zero(t, legacyCalls)
			require.Zero(t, info.SubscriptionQuotaScale)
			require.Zero(t, info.SubscriptionId)
			wallet, err := GetUserClaudeWalletQuota(info.UserId)
			require.NoError(t, err)
			require.Equal(t, 10000, wallet)
		})
	}
}

func TestCreditPackPriorityTokenFallbackRefundsBeforeLegacyAndKeepsPricing(t *testing.T) {
	ctx, info := creditPackPriorityContext(t, "subscription_only", 0)
	seedToken(t, info.UserId+1000000, info.UserId, "priority-token", 50)
	info.IsPlayground, info.TokenId, info.TokenKey = false, info.UserId+1000000, "priority-token"
	info.ChannelMeta.MultiplierCardUserEnabled = true
	previousSQLite := platformdb.UsingSQLite
	platformdb.UsingSQLite = false
	t.Cleanup(func() { platformdb.UsingSQLite = previousSQLite })
	var packRequest, legacyRequest string
	var calls []string
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsumeCreditPack: func(request string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
			packRequest = request
			calls = append(calls, "pack")
			require.EqualValues(t, 100, amount)
			return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 22, PreConsumed: amount, ReservationID: "pack", PlanID: 2, CreditPackGrantedQuota: 103000, CreditPackPaidQuota: 100000, CreditPackChannelShareBPS: 6000}, nil
		},
		RefundPreConsume:               func(request string) error { calls = append(calls, "refund:"+request); return nil },
		GetMonthlyPassEntitlement:      testMonthlyPassEntitlement,
		ValidateMonthlyPassEntitlement: testValidMonthlyPassEntitlement,
		PreConsume: func(request string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
			legacyRequest = request
			calls = append(calls, "legacy")
			require.EqualValues(t, 25, amount)
			return &SubscriptionFundingPreConsumeResult{UserSubscriptionID: 11, PreConsumed: amount, ReservationID: "legacy", PlanID: 1}, nil
		},
		PostConsumeDelta: func(_ int, _ string, delta int64) error { require.EqualValues(t, -22, delta); return nil },
	})
	require.Nil(t, PreConsumeRelayBilling(ctx, 100, info))
	require.Equal(t, []string{"pack", "refund:" + packRequest, "legacy"}, calls)
	require.NotEqual(t, legacyRequest, packRequest)
	require.Equal(t, info.RequestId, legacyRequest)
	require.Equal(t, 11, info.SubscriptionId)
	require.Zero(t, info.CreditPackGrantedQuota)
	require.Equal(t, .5, info.SubscriptionGroupMultiplier)
	require.Equal(t, .1, info.SubscriptionPackageMultiplier)
	require.Equal(t, .025, info.SubscriptionQuotaScale)
	// Retrying an already reserved request must not select or charge a pack again.
	require.Nil(t, PreConsumeRelayBilling(ctx, 100, info))
	require.Len(t, calls, 3)
	require.NoError(t, info.Billing.Settle(100))
	var token identityschema.Token
	require.NoError(t, platformdb.DB.First(&token, info.TokenId).Error)
	require.Equal(t, 47, token.RemainQuota)
	require.Equal(t, 3, token.UsedQuota)
}

func TestCreditPackPriorityTokenRollbackFailureCannotFallBack(t *testing.T) {
	ctx, info := creditPackPriorityContext(t, "subscription_first", 10000)
	seedToken(t, info.UserId+1000000, info.UserId, "priority-rollback", 1000)
	info.IsPlayground, info.TokenId, info.TokenKey = false, info.UserId+1000000, "priority-rollback"
	const callback = "priority-token-refund-failure"
	rollbackFailure := errors.New("injected token rollback failure")
	require.NoError(t, platformdb.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "tokens" {
			return
		}
		values, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		// Only reject the compensating increase, after the initial debit succeeded.
		if expression, ok := values["remain_quota"].(clause.Expr); ok && expression.SQL == "remain_quota + ?" {
			tx.AddError(rollbackFailure)
		}
	}))
	t.Cleanup(func() { _ = platformdb.DB.Callback().Update().Remove(callback) })
	legacyCalls := 0
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsumeCreditPack: func(string, int, string, int64) (*SubscriptionFundingPreConsumeResult, error) {
			return nil, billingdomain.ErrInsufficientBalance
		},
		PreConsume: func(string, int, string, int64) (*SubscriptionFundingPreConsumeResult, error) {
			legacyCalls++
			return nil, errors.New("unexpected legacy attempt")
		},
	})
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, session)
	require.NotNil(t, apiErr)
	require.ErrorIs(t, apiErr, rollbackFailure)
	require.Equal(t, types.ErrorCodeUpdateDataError, apiErr.GetErrorCode())
	require.Zero(t, legacyCalls)
}
