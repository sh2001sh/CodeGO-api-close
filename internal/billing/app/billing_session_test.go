package app

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestBillingSessionRefundSyncRestoresWalletAndTokenPreConsume(t *testing.T) {
	truncate(t)
	seedUser(t, 1001, 10000)
	seedToken(t, 2001, 1001, "sk-refund", 10000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          1001,
		UserQuota:       10000,
		TokenId:         2001,
		TokenKey:        "sk-refund",
		OriginModelName: "gpt-5",
		RequestId:       "req-refund",
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}

	session, apiErr := NewBillingSession(ctx, info, 3000)
	require.Nil(t, apiErr)
	require.Equal(t, 3000, session.GetPreConsumedQuota())

	userQuota, err := GetUserClaudeWalletQuota(1001)
	require.NoError(t, err)
	require.Equal(t, 7000, userQuota)
	require.Equal(t, int64(7000), loadBillingSnapshot(t, 1001, "claude_wallet").AvailableBalance)

	require.NoError(t, session.RefundSync(ctx))

	userQuota, err = GetUserClaudeWalletQuota(1001)
	require.NoError(t, err)
	require.Equal(t, 10000, userQuota)
	require.Equal(t, int64(10000), loadBillingSnapshot(t, 1001, "claude_wallet").AvailableBalance)
}

func TestBillingSessionSettleAdjustsWalletAndTokenToActualUsage(t *testing.T) {
	truncate(t)
	seedUser(t, 1002, 10000)
	seedToken(t, 2002, 1002, "sk-settle", 10000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          1002,
		UserQuota:       10000,
		TokenId:         2002,
		TokenKey:        "sk-settle",
		OriginModelName: "gpt-5",
		RequestId:       "req-settle",
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}

	session, apiErr := NewBillingSession(ctx, info, 3000)
	require.Nil(t, apiErr)

	require.NoError(t, session.Settle(4500))

	userQuota, err := GetUserClaudeWalletQuota(1002)
	require.NoError(t, err)
	require.Equal(t, 5500, userQuota)
	snapshot := loadBillingSnapshot(t, 1002, "claude_wallet")
	require.Equal(t, int64(5500), snapshot.AvailableBalance)
	require.Equal(t, int64(4500), snapshot.ConsumedTotal)
	require.NoError(t, session.RefundSync(ctx))
	userQuota, err = GetUserClaudeWalletQuota(1002)
	require.NoError(t, err)
	require.Equal(t, 5500, userQuota)
}

func TestBillingSessionSettlementCapsAtReservedQuotaWhenBalanceIsInsufficient(t *testing.T) {
	truncate(t)
	seedUser(t, 1090, 10_000)
	seedToken(t, 2090, 1090, "sk-capped-settle", 10_000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 1090, TokenId: 2090, TokenKey: "sk-capped-settle", OriginModelName: "gpt-5",
		RequestId: "req-capped-settle", IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
	session, apiErr := NewBillingSession(ctx, info, 3_000)
	require.Nil(t, apiErr)

	require.NoError(t, session.Settle(12_000))
	require.True(t, info.BillingSettled)
	require.Equal(t, 10_000, info.BillingSettledQuota)
	require.Equal(t, 10_000, BillingQuotaForLog(info, 12_000))

	snapshot := loadBillingSnapshot(t, 1090, billingAccountTypeClaudeWallet)
	require.Zero(t, snapshot.AvailableBalance)
	require.EqualValues(t, 10_000, snapshot.ConsumedTotal)
}

func TestBillingSessionReserveDoesNotDoubleCountSubscriptionAdditionalQuota(t *testing.T) {
	originalHooks := subscriptionFundingHooks
	t.Cleanup(func() { subscriptionFundingHooks = originalHooks })
	subscriptionFundingHooks.ReserveAdditional = func(string, int, string, int64) error { return nil }

	info := &relaycommon.RelayInfo{}
	funding := &SubscriptionFunding{requestID: "req-sub-retry", subscriptionID: 10, preConsumed: 100, AmountUsedAfter: 100}
	session := &BillingSession{relayInfo: info, funding: funding, preConsumedQuota: 100}

	require.NoError(t, session.Reserve(150))
	require.EqualValues(t, 150, info.SubscriptionPreConsumed)
	require.EqualValues(t, 150, info.SubscriptionAmountUsedAfterPreConsume)
}

func TestBillingSessionUsesOneReservationForRelayLifecycle(t *testing.T) {
	truncate(t)
	seedUser(t, 1012, 10000)
	seedToken(t, 2012, 1012, "sk-ledger-lifecycle", 10000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          1012,
		TokenId:         2012,
		TokenKey:        "sk-ledger-lifecycle",
		OriginModelName: "gpt-5",
		RequestId:       "req-ledger-lifecycle",
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
	}

	session, apiErr := NewBillingSession(ctx, info, 3000)
	require.Nil(t, apiErr)
	funding, ok := session.funding.(*LedgerRelayFunding)
	require.True(t, ok)
	require.NotEmpty(t, funding.ReservationID())

	var reservation billingschema.BillingReservation
	require.NoError(t, platformdb.DB.Where("reservation_id = ?", funding.ReservationID()).First(&reservation).Error)
	require.Equal(t, "req-ledger-lifecycle", reservation.RequestID)
	require.Equal(t, billingschema.BillingReservationStatusOpen, reservation.Status)

	require.NoError(t, session.Settle(2500))
	require.NotEmpty(t, funding.SettlementID())
	require.NoError(t, platformdb.DB.Where("reservation_id = ?", funding.ReservationID()).First(&reservation).Error)
	require.Equal(t, billingschema.BillingReservationStatusSettled, reservation.Status)

	var settlement billingschema.BillingSettlement
	require.NoError(t, platformdb.DB.Where("settlement_id = ?", funding.SettlementID()).First(&settlement).Error)
	require.Equal(t, funding.ReservationID(), settlement.ReservationID)
	require.EqualValues(t, 2500, settlement.ActualAmount)
}

func TestBillingSessionSettlesExactPreConsumeIntoLedger(t *testing.T) {
	truncate(t)
	seedUser(t, 1014, 10_000)
	seedToken(t, 2014, 1014, "sk-exact-ledger-settle", 10_000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 1014, TokenId: 2014, TokenKey: "sk-exact-ledger-settle", OriginModelName: "gpt-5",
		RequestId: "req-exact-ledger-settle", IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
	session, apiErr := NewBillingSession(ctx, info, 3_000)
	require.Nil(t, apiErr)
	require.NoError(t, session.Settle(3_000))

	funding := session.funding.(*LedgerRelayFunding)
	var reservation billingschema.BillingReservation
	require.NoError(t, platformdb.DB.Where("reservation_id = ?", funding.ReservationID()).First(&reservation).Error)
	require.Equal(t, billingschema.BillingReservationStatusSettled, reservation.Status)
	var settlement billingschema.BillingSettlement
	require.NoError(t, platformdb.DB.Where("reservation_id = ?", funding.ReservationID()).First(&settlement).Error)
	require.EqualValues(t, 3_000, settlement.ActualAmount)
}

func TestUnlimitedTokenWalletSessionStillPreConsumes(t *testing.T) {
	truncate(t)
	const trustedQuota = 6_000_000
	seedUser(t, 1015, trustedQuota)
	require.NoError(t, platformdb.DB.Create(&identityschema.Token{
		Id: 2015, UserId: 1015, Key: "sk-trusted-ledger-settle", Name: "trusted", Status: constant.TokenStatusEnabled,
		UnlimitedQuota: true,
	}).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 1015, UserQuota: trustedQuota, TokenId: 2015, TokenKey: "sk-trusted-ledger-settle", TokenUnlimited: true,
		OriginModelName: "gpt-5", RequestId: "req-trusted-ledger-settle", IsPlayground: true,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
	session, apiErr := NewBillingSession(ctx, info, 1_000)
	require.Nil(t, apiErr)
	require.Equal(t, 1_000, session.GetPreConsumedQuota())
	require.NoError(t, session.Settle(1_000))

	funding := session.funding.(*LedgerRelayFunding)
	var reservation billingschema.BillingReservation
	require.NoError(t, platformdb.DB.Where("reservation_id = ?", funding.ReservationID()).First(&reservation).Error)
	require.Equal(t, billingschema.BillingReservationStatusSettled, reservation.Status)
	var settlement billingschema.BillingSettlement
	require.NoError(t, platformdb.DB.Where("reservation_id = ?", funding.ReservationID()).First(&settlement).Error)
	require.EqualValues(t, 1_000, settlement.ActualAmount)
}

func TestBillingSessionRequestReplayDoesNotProjectWalletTwice(t *testing.T) {
	truncate(t)
	seedUser(t, 1013, 10000)
	seedToken(t, 2013, 1013, "sk-ledger-replay", 10000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          1013,
		TokenId:         2013,
		TokenKey:        "sk-ledger-replay",
		OriginModelName: "gpt-5",
		RequestId:       "req-ledger-replay",
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
	}

	first, apiErr := NewBillingSession(ctx, info, 3000)
	require.Nil(t, apiErr)
	second, apiErr := NewBillingSession(ctx, info, 3000)
	require.Nil(t, apiErr)

	firstFunding := first.funding.(*LedgerRelayFunding)
	secondFunding := second.funding.(*LedgerRelayFunding)
	require.Equal(t, firstFunding.ReservationID(), secondFunding.ReservationID())
	userQuota, err := GetUserClaudeWalletQuota(1013)
	require.NoError(t, err)
	require.Equal(t, 7000, userQuota)
}

func TestPreConsumeRelayBillingReusesSessionAcrossChannelRetry(t *testing.T) {
	truncate(t)
	seedUser(t, 1016, 10_000)
	seedToken(t, 2016, 1016, "sk-retry-billing", 10_000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          1016,
		TokenId:         2016,
		TokenKey:        "sk-retry-billing",
		OriginModelName: "gpt-5",
		RequestId:       "req-retry-billing",
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
	}

	require.Nil(t, PreConsumeRelayBilling(ctx, 3_000, info))
	firstSession := info.Billing
	require.NotNil(t, firstSession)
	require.Nil(t, PreConsumeRelayBilling(ctx, 2_000, info))
	require.Same(t, firstSession, info.Billing)

	quota, err := GetUserClaudeWalletQuota(1016)
	require.NoError(t, err)
	require.Equal(t, 7_000, quota)
	require.NoError(t, RefundRelayBillingSync(ctx, info))

	quota, err = GetUserClaudeWalletQuota(1016)
	require.NoError(t, err)
	require.Equal(t, 10_000, quota)
}

func TestBridgeUsesSingleUnifiedCreditLedgerAccount(t *testing.T) {
	truncate(t)
	require.NoError(t, platformdb.DB.Create(&identityschema.User{
		Id:          1005,
		Username:    "unified_credit_user",
		ClaudeQuota: 2000,
		Status:      constant.UserStatusEnabled,
	}).Error)

	require.NoError(t, AdjustClaudeWalletQuota(1005, 500))

	claudeSnapshot := loadBillingSnapshot(t, 1005, "claude_wallet")
	require.Equal(t, int64(1500), claudeSnapshot.AvailableBalance)

	var accounts []billingschema.BillingAccount
	require.NoError(t, platformdb.DB.Where("owner_type = ? AND owner_id = ?", "user", 1005).Order("account_type asc").Find(&accounts).Error)
	require.Len(t, accounts, 1)
	require.Equal(t, "claude_wallet", accounts[0].AccountType)
}

func TestAdjustUnifiedCreditConsumesBonusQuotaCredits(t *testing.T) {
	truncate(t)
	require.NoError(t, platformdb.DB.Create(&identityschema.User{
		Id:          1006,
		Username:    "bonus_wallet_user",
		ClaudeQuota: 1000,
		Status:      constant.UserStatusEnabled,
	}).Error)
	require.NoError(t, platformdb.DB.Create(&billingschema.BonusQuotaCredit{
		UserId:          1006,
		OriginalAmount:  1000,
		RemainingAmount: 1000,
		SourceType:      billingschema.PointSourceBonusConversion,
		SourceId:        "seed-bonus-credit",
		IdempotencyKey:  "seed-bonus-credit",
		Status:          billingschema.BonusQuotaStatusActive,
	}).Error)

	require.NoError(t, AdjustClaudeWalletQuota(1006, 400))

	userQuota, err := GetUserClaudeWalletQuota(1006)
	require.NoError(t, err)
	require.Equal(t, 600, userQuota)

	var credit billingschema.BonusQuotaCredit
	require.NoError(t, platformdb.DB.Where("user_id = ?", 1006).First(&credit).Error)
	require.EqualValues(t, 600, credit.RemainingAmount)
	require.Equal(t, billingschema.BonusQuotaStatusActive, credit.Status)

	require.NoError(t, AdjustClaudeWalletQuota(1006, 600))

	require.NoError(t, platformdb.DB.Where("user_id = ?", 1006).First(&credit).Error)
	require.Zero(t, credit.RemainingAmount)
	require.Equal(t, billingschema.BonusQuotaStatusExhausted, credit.Status)
}

func TestNewBillingSessionReturnsUnifiedCreditMessage(t *testing.T) {
	truncate(t)
	seedUser(t, 1003, 750)
	seedToken(t, 2003, 1003, "sk-wallet-insufficient", 10000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          1003,
		TokenId:         2003,
		TokenKey:        "sk-wallet-insufficient",
		OriginModelName: "gpt-5",
		RequestId:       "req-wallet-insufficient",
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}

	session, apiErr := NewBillingSession(ctx, info, 2364)
	require.Nil(t, session)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	require.Equal(t,
		"通用额度不足, 当前余额: "+logger.FormatQuota(750)+", 本次所需: "+logger.FormatQuota(2364),
		apiErr.Error(),
	)
}

func TestNewBillingSessionReturnsCombinedFundingMessage(t *testing.T) {
	truncate(t)
	previousHooks := subscriptionFundingHooks
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		PreConsume: func(string, int, string, int64) (*SubscriptionFundingPreConsumeResult, error) {
			return nil, errors.New("subscription quota insufficient")
		},
	})
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(previousHooks) })
	seedUser(t, 1005, 750)
	seedToken(t, 2005, 1005, "sk-combined-insufficient", 10000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          1005,
		TokenId:         2005,
		TokenKey:        "sk-combined-insufficient",
		OriginModelName: "gpt-5",
		RequestId:       "req-combined-insufficient",
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
	}

	session, apiErr := NewBillingSession(ctx, info, 2364)
	require.Nil(t, session)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	require.Equal(t,
		"通用额度不足, 当前余额: "+logger.FormatQuota(750)+", 本次所需: "+logger.FormatQuota(2364),
		apiErr.Error(),
	)
}

func TestNewBillingSessionReturnsLocalClaudeQuotaMessage(t *testing.T) {
	truncate(t)
	require.NoError(t, platformdb.DB.Model(&identityschema.User{}).Create(&identityschema.User{
		Id:          1004,
		Username:    "claude_user",
		ClaudeQuota: 750,
		Status:      constant.UserStatusEnabled,
	}).Error)
	seedToken(t, 2004, 1004, "sk-claude-insufficient", 10000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          1004,
		TokenId:         2004,
		TokenKey:        "sk-claude-insufficient",
		OriginModelName: "claude-3-7-sonnet",
		RequestId:       "req-claude-insufficient",
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				ClaudeWalletEnabled: true,
			},
		},
	}

	session, apiErr := NewBillingSession(ctx, info, 2364)
	require.Nil(t, session)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	require.Equal(t,
		"通用额度不足, 当前余额: "+logger.FormatQuota(750)+", 本次所需: "+logger.FormatQuota(2364),
		apiErr.Error(),
	)
}

func TestFundingInsufficientErrorUsesFundingSource(t *testing.T) {
	subscriptionErr := newFundingInsufficientError(BillingSourceSubscription, billingdomain.ErrInsufficientBalance)
	require.NotNil(t, subscriptionErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, subscriptionErr.GetErrorCode())
	require.Contains(t, subscriptionErr.Error(), "subscription quota insufficient")
	require.NotContains(t, subscriptionErr.Error(), "blind box")

	blindBoxErr := newFundingInsufficientError(BillingSourceSubscription, commercedomain.ErrBlindBoxInsufficientQuota)
	require.NotNil(t, blindBoxErr)
	require.Contains(t, blindBoxErr.Error(), "blind box quota insufficient")
}

func loadBillingSnapshot(t *testing.T, userID int, accountType string) *billingschema.BillingBalanceSnapshot {
	t.Helper()
	var account billingschema.BillingAccount
	require.NoError(t, platformdb.DB.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user", userID, accountType).First(&account).Error)

	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, platformdb.DB.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	return &snapshot
}
