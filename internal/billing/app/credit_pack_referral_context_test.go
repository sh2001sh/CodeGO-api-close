package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func referralPackRelayInfo(sub commerceschema.UserSubscription, accountID, fundingRequest, originalRequest string, market bool) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{RequestId: originalRequest, UserId: sub.UserId, BillingSource: BillingSourceSubscription,
		CreditPackGrantedQuota: sub.CreditPackGrantedQuota, CreditPackPaidQuota: sub.CreditPackPaidQuota, CreditPackChannelShareBPS: sub.CreditPackChannelShareBPS,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 123, ChannelScope: gatewayschema.ChannelScopeOfficial},
	}
	if market {
		info.ChannelScope = gatewayschema.ChannelScopeExternal
		info.MarketplaceGroupID = "market-group"
		info.MarketplaceOwnerID = 1
		info.MarketplaceMultiplier = 1
	}
	info.Billing = &BillingSession{funding: &SubscriptionFunding{requestID: fundingRequest, accountID: accountID, creditPack: true}}
	return info
}

func referralPackAccount(t *testing.T, db *gorm.DB, granted, paid int64) (commerceschema.UserSubscription, string) {
	t.Helper()
	sub := commerceschema.UserSubscription{UserId: 2, AmountTotal: granted, CreditPackGrantedQuota: granted, CreditPackPaidQuota: paid, CreditPackChannelShareBPS: 6000}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{UserId: 2, TradeNo: "purchase-pack", Money: float64(paid), Status: "success", TargetSubscriptionId: sub.Id, CreditPackQuota: granted, CreditPackPaidQuota: paid}).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id)})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: granted, IdempotencyKey: "purchase-pack-credit"})
	require.NoError(t, err)
	return sub, account.AccountID
}

func TestCreditPackReferralDebitsOwnerEarningsBeforeRelease(t *testing.T) {
	for _, lateMarket := range []bool{false, true} {
		t.Run(fmt.Sprintf("late_market_%v", lateMarket), func(t *testing.T) {
			db, now := referralTestDB(t)
			sub, accountID := referralPackAccount(t, db, 110000, 100000)
			fundingRequest := "cp:hashed-funding-identity"
			native := referralSettle(t, db, accountID, fundingRequest, "subscription:"+fundingRequest, 110000, 110000, now)
			info := referralPackRelayInfo(sub, accountID, fundingRequest, "relay-original", true)
			params := marketplaceSettlementParams(info, 110000)
			if !lateMarket {
				require.NoError(t, marketplacesettlement.Record(params))
				marketplacesettlement.RegisterReleaseHook(CreditMarketplaceOwnerEarningsTx)
				t.Cleanup(func() { marketplacesettlement.RegisterReleaseHook(nil) })
				_, err := marketplacesettlement.ReleasePending(marketplacesettlement.ReleaseFilter{})
				require.ErrorContains(t, err, "referral allocation is pending")
			}
			// The outbox can run before route attachment; it must not fund a free reward.
			_, err := RunLedgerWorkerBatch(context.Background(), 100)
			require.NoError(t, err)
			var event billingschema.BillingOutboxEvent
			require.NoError(t, db.First(&event, "aggregate_id = ?", native.SettlementID).Error)
			require.Equal(t, "pending", event.Status)
			require.Contains(t, event.LastError, "route context is not ready")
			var receipts int64
			require.NoError(t, db.Model(&billingschema.ReferralConsumptionRebate{}).Count(&receipts).Error)
			require.Zero(t, receipts)
			require.NoError(t, attachCreditPackReferralContext(info, 110000))
			require.NoError(t, db.First(&event, "aggregate_id = ?", native.SettlementID).Error)
			var compatible billingschema.BillingSettlement
			require.NoError(t, json.Unmarshal(event.Payload, &compatible))
			require.Equal(t, native.SettlementID, compatible.SettlementID)
			_, err = processLedgerOutboxAccount(context.Background(), accountID)
			require.NoError(t, err)
			var market marketplaceschema.Settlement
			require.NoError(t, db.First(&market, "request_id = ?", info.RequestId).Error)
			require.EqualValues(t, 95500, market.OwnerNetAmount)
			require.EqualValues(t, 3000, market.ReferralRebateAmount)
			require.False(t, market.ReferralRebatePending)
			require.EqualValues(t, 110000, market.PlatformCommission+market.TransactionFee+market.ReferralRebateAmount+market.OwnerNetAmount)
			var pending billingschema.BillingBalanceSnapshot
			require.NoError(t, db.First(&pending, "account_id = ?", market.PendingAccountID).Error)
			require.EqualValues(t, 95500, pending.AvailableBalance)
			var receipt billingschema.ReferralConsumptionRebate
			require.NoError(t, db.First(&receipt, "settlement_id = ?", native.SettlementID).Error)
			require.Equal(t, market.ID, receipt.MarketplaceSettlementID)
			require.Equal(t, "marketplace_channel", receipt.FundingSource)
			require.EqualValues(t, 3000, receipt.RebateQuota)
			var inviter identityschema.User
			require.NoError(t, db.First(&inviter, 1).Error)
			require.Equal(t, 3000, inviter.ClaudeQuota)
			// Repeat both projection and event; neither owner debit nor reward repeats.
			require.NoError(t, marketplacesettlement.Record(params))
			require.NoError(t, db.Model(&event).Update("status", "pending").Error)
			_, err = processLedgerOutboxAccount(context.Background(), accountID)
			require.NoError(t, err)
			marketplacesettlement.RegisterReleaseHook(CreditMarketplaceOwnerEarningsTx)
			t.Cleanup(func() { marketplacesettlement.RegisterReleaseHook(nil) })
			_, err = marketplacesettlement.ReleasePending(marketplacesettlement.ReleaseFilter{})
			require.NoError(t, err)
			require.NoError(t, db.First(&inviter, 1).Error)
			require.Equal(t, 98500, inviter.ClaudeQuota) // Same controller: 95.5 + 3 = 98.5.
		})
	}
}

func TestCreditPackReferralMultipleReservationsKeepGateUntilEveryReceipt(t *testing.T) {
	db, now := referralTestDB(t)
	sub, accountID := referralPackAccount(t, db, 110, 100)
	const request = "cp:multi-reservation"
	var native []billingschema.BillingSettlement
	for index := 0; index < 11; index++ {
		native = append(native, referralSettle(t, db, accountID, request, fmt.Sprintf("subscription:%s:chunk-%d", request, index), 10, 10, now))
	}
	info := referralPackRelayInfo(sub, accountID, request, "original-multi", true)
	require.NoError(t, attachCreditPackReferralContext(info, 110))
	for index, item := range native {
		var event billingschema.BillingOutboxEvent
		require.NoError(t, db.First(&event, "aggregate_id = ?", item.SettlementID).Error)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return consumeReferralSettlementTx(tx, event, make(map[int]struct{})) }))
		var market marketplaceschema.Settlement
		require.NoError(t, db.First(&market, "request_id = ?", info.RequestId).Error)
		require.Equal(t, index != len(native)-1, market.ReferralRebatePending)
	}
	var total struct{ Eligible, Rebate int64 }
	require.NoError(t, db.Model(&billingschema.ReferralConsumptionRebate{}).Select("SUM(eligible_quota) AS eligible, SUM(rebate_quota) AS rebate").Scan(&total).Error)
	require.EqualValues(t, 100, total.Eligible)
	require.EqualValues(t, 3, total.Rebate)
	var market marketplaceschema.Settlement
	require.NoError(t, db.First(&market, "request_id = ?", info.RequestId).Error)
	require.EqualValues(t, 95, market.OwnerNetAmount) // Integer fee rounding: 110 - 6 - 6 - 3.
	require.EqualValues(t, 3, market.ReferralRebateAmount)
}

func TestCreditPackReferralMissingOfficialEvidenceAndFundingFailureAreRetryable(t *testing.T) {
	db, now := referralTestDB(t)
	sub, accountID := referralPackAccount(t, db, 110000, 100000)
	const request = "cp:funding-failure"
	native := referralSettle(t, db, accountID, request, "subscription:"+request, 110000, 110000, now)
	info := referralPackRelayInfo(sub, accountID, request, "original-failure", false)
	info.ChannelId = 0
	require.ErrorContains(t, attachCreditPackReferralContext(info, 110000), "route evidence is missing")
	info = referralPackRelayInfo(sub, accountID, request, "original-failure", true)
	require.NoError(t, attachCreditPackReferralContext(info, 110000))
	require.NoError(t, marketplacesettlement.Record(marketplaceSettlementParams(info, 110000)))
	var market marketplaceschema.Settlement
	require.NoError(t, db.First(&market, "request_id = ?", info.RequestId).Error)
	// Simulate frozen/unavailable source funds: no reward, receipt, or readiness
	// flag may commit before the exact owner debit succeeds.
	const callback = "fail-marketplace-referral-reserve"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*billingschema.BillingReservation); ok && row.AccountID == market.PendingAccountID {
			tx.AddError(errors.New("injected owner referral funding failure"))
		}
	}))
	_, err := RunLedgerWorkerBatch(context.Background(), 100)
	require.NoError(t, err)
	var event billingschema.BillingOutboxEvent
	require.NoError(t, db.First(&event, "aggregate_id = ?", native.SettlementID).Error)
	require.Equal(t, "pending", event.Status)
	require.Contains(t, event.LastError, "injected owner referral funding failure")
	var receiptCount int64
	require.NoError(t, db.Model(&billingschema.ReferralConsumptionRebate{}).Count(&receiptCount).Error)
	require.Zero(t, receiptCount)
	require.NoError(t, db.First(&market, "request_id = ?", info.RequestId).Error)
	require.True(t, market.ReferralRebatePending)
	require.EqualValues(t, 98500, market.OwnerNetAmount)
	require.NoError(t, db.Callback().Create().Remove(callback))
	_, err = processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	require.NoError(t, db.First(&market, "request_id = ?", info.RequestId).Error)
	require.False(t, market.ReferralRebatePending)
	require.EqualValues(t, 95500, market.OwnerNetAmount)
}

func TestCreditPackReferralNoInviteStillCompletesOwnerGate(t *testing.T) {
	db, now := referralTestDB(t)
	require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", 2).Update("inviter_id", 0).Error)
	sub, accountID := referralPackAccount(t, db, 110000, 100000)
	referralSettle(t, db, accountID, "cp:no-invite", "subscription:cp:no-invite", 110000, 110000, now)
	info := referralPackRelayInfo(sub, accountID, "cp:no-invite", "original-no-invite", true)
	require.NoError(t, attachCreditPackReferralContext(info, 110000))
	_, err := processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	var market marketplaceschema.Settlement
	require.NoError(t, db.First(&market, "request_id = ?", info.RequestId).Error)
	require.False(t, market.ReferralRebatePending)
	require.Zero(t, market.ReferralRebateAmount)
	require.EqualValues(t, 98500, market.OwnerNetAmount)
	var receipt billingschema.ReferralConsumptionRebate
	require.NoError(t, db.First(&receipt).Error)
	require.Equal(t, "referral_not_eligible", receipt.ExclusionReason)
	require.Zero(t, receipt.RebateQuota)
}

func TestCreditPackReferralCarriesWalletRemainderIntoMicroCalls(t *testing.T) {
	db, now := referralTestDB(t)
	walletID := referralPaidWallet(t, db, 33, 33)
	referralSettle(t, db, walletID, "wallet-carry", "relay:wallet:wallet-carry", 33, 33, now)
	_, err := processLedgerOutboxAccount(context.Background(), walletID)
	require.NoError(t, err)
	sub, accountID := referralPackAccount(t, db, 110, 100)
	for index := 0; index < 40; index++ {
		request := fmt.Sprintf("cp:micro-%d", index)
		referralSettle(t, db, accountID, request, "subscription:"+request, 1, 1, now)
		info := referralPackRelayInfo(sub, accountID, request, fmt.Sprintf("original-micro-%d", index), true)
		require.NoError(t, attachCreditPackReferralContext(info, 1))
		_, err = processLedgerOutboxAccount(context.Background(), accountID)
		require.NoError(t, err)
		var market marketplaceschema.Settlement
		require.NoError(t, db.First(&market, "request_id = ?", info.RequestId).Error)
		require.False(t, market.ReferralRebatePending)
		require.GreaterOrEqual(t, market.OwnerNetAmount, int64(0))
		require.EqualValues(t, 1, market.OwnerNetAmount+market.ReferralRebateAmount)
	}
	marketplacesettlement.RegisterReleaseHook(CreditMarketplaceOwnerEarningsTx)
	t.Cleanup(func() { marketplacesettlement.RegisterReleaseHook(nil) })
	_, err = marketplacesettlement.ReleasePending(marketplacesettlement.ReleaseFilter{})
	require.NoError(t, err)
	var pending int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Where("status = ?", "pending").Count(&pending).Error)
	require.Zero(t, pending)
}
