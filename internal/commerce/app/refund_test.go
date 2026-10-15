package app

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRefundableTopupUsesFundingLotQuotaUnits(t *testing.T) {
	originalDB := platformdb.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })
	require.NoError(t, db.AutoMigrate(
		&commerceschema.TopUp{},
		&billingschema.BillingAccount{},
		&billingschema.FundingLot{},
	))

	const userID = 5722
	const tradeNo = "refund-quota-units"
	account := billingschema.BillingAccount{AccountID: "refund-wallet", OwnerType: "user", OwnerID: userID, AccountType: "claude_wallet", QuotaUnit: "quota"}
	require.NoError(t, db.Create(&account).Error)
	require.NoError(t, db.Create(&billingschema.FundingLot{
		AccountID:       account.AccountID,
		Source:          billingschema.FundingSourceTopup,
		OriginalAmount:  50_000_000,
		RemainingAmount: 50_000_000,
		IdempotencyKey:  fmt.Sprintf("topup:%s:unified", tradeNo),
	}).Error)
	order := commerceschema.TopUp{UserId: userID, TradeNo: tradeNo, Amount: 100, Money: 100, ProviderPayload: `{"trade_no":"provider-order"}`}

	item := refundableTopup(userID, &order)

	require.EqualValues(t, 50_000_000, item.TotalQuota)
	require.Zero(t, item.UsedQuota)
	require.EqualValues(t, 50_000_000, item.RemainingQuota)
	require.Equal(t, 100.0, item.GrossRefund)
	require.Equal(t, 2.0, item.FeeAmount)
	require.Equal(t, 98.0, item.RefundAmount)
	require.True(t, item.Refundable)
}

func TestRefundMoneyDeductsTwoPercentFee(t *testing.T) {
	gross, fee, net := refundMoney(169, 90, 100)
	require.Equal(t, 152.1, gross)
	require.Equal(t, 3.04, fee)
	require.Equal(t, 149.06, net)
}

func TestRefundMoneyRejectsEmptyQuota(t *testing.T) {
	gross, fee, net := refundMoney(100, 0, 50_000_000)
	require.Zero(t, gross)
	require.Zero(t, fee)
	require.Zero(t, net)
}

func TestRefundableSubscriptionKeepsUsageClearedByReferralReset(t *testing.T) {
	db := setupRedemptionTestDB(t)

	const (
		userID         = 5723
		subscriptionID = 6723
	)
	subscription := commerceschema.UserSubscription{
		Id: subscriptionID, UserId: userID, PlanId: 1,
		AmountTotal: 1_000, AmountUsed: 0,
		Status: "active",
	}
	require.NoError(t, db.Create(&subscription).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityLedger{
		UserId: userID, RelatedUserId: subscriptionID,
		ChangeType: commerceschema.SubscriptionResetOpportunityChangeUse,
		Delta:      -1, UsedMonth: "2026-09", SourceType: "user_subscription",
		SourceRef: "6723", EventKey: "refund-after-reset-6723",
	}).Error)
	account := billingschema.BillingAccount{
		AccountID: "refund-reset-subscription", OwnerType: "user_subscription",
		OwnerID: subscriptionID, AccountType: "subscription", QuotaUnit: "quota",
	}
	require.NoError(t, db.Create(&account).Error)
	require.NoError(t, db.Create(&billingschema.BillingBalanceSnapshot{
		AccountID: account.AccountID, ConsumedTotal: 400, RefundedTotal: 50,
	}).Error)
	order := commerceschema.SubscriptionOrder{
		UserId: userID, TargetSubscriptionId: subscriptionID, Money: 100,
		ProviderPayload: `{"trade_no":"provider-subscription-order"}`,
	}

	item := refundableSubscription(userID, &order)

	require.EqualValues(t, 1_000, item.TotalQuota)
	require.EqualValues(t, 350, item.UsedQuota)
	require.EqualValues(t, 650, item.RemainingQuota)
	require.Equal(t, 65.0, item.GrossRefund)
	require.Equal(t, 1.3, item.FeeAmount)
	require.Equal(t, 63.7, item.RefundAmount)
	require.True(t, item.Refundable)
}

func TestRefundableSubscriptionFailsClosedWhenResetUsageCannotBeVerified(t *testing.T) {
	db := setupRedemptionTestDB(t)

	const (
		userID         = 5724
		subscriptionID = 6724
	)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{
		Id: subscriptionID, UserId: userID, PlanId: 1,
		AmountTotal: 1_000, AmountUsed: 0, Status: "active",
	}).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityLedger{
		UserId: userID, RelatedUserId: subscriptionID,
		ChangeType: commerceschema.SubscriptionResetOpportunityChangeUse,
		Delta:      -1, UsedMonth: "2026-09", SourceType: "user_subscription",
		SourceRef: "6724", EventKey: "refund-after-reset-6724",
	}).Error)
	order := commerceschema.SubscriptionOrder{
		UserId: userID, TargetSubscriptionId: subscriptionID, Money: 100,
		ProviderPayload: `{"trade_no":"provider-subscription-order"}`,
	}

	item := refundableSubscription(userID, &order)

	require.False(t, item.Refundable)
	require.Contains(t, item.UnavailableReason, "无法核验累计消耗")
}

func TestFinalizeSubscriptionRefundClosesPackageAfterPendingUsage(t *testing.T) {
	db := setupRedemptionTestDB(t)

	const (
		userID         = 5725
		subscriptionID = 6725
		tradeNo        = "refund-pending-usage"
		refundNo       = "refund-pending-usage-provider"
	)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{
		Id: subscriptionID, UserId: userID, PlanId: 1,
		AmountTotal: 1_000, AmountUsed: 950,
		PeriodAmount: 1_000, PeriodUsed: 950, Status: "active",
	}).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
		UserId: userID, TradeNo: tradeNo, TargetSubscriptionId: subscriptionID,
		RefundStatus: commerceschema.RefundStatusProcessing, RefundQuota: 400,
	}).Error)

	require.NoError(t, finalizeUserRefund(userID, commerceschema.RefundOrderTypeSubscription, tradeNo, refundNo, 39.2))

	var subscription commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", subscriptionID).First(&subscription).Error)
	require.Equal(t, commerceschema.SubscriptionStatusSettledCancelled, subscription.Status)
	require.EqualValues(t, 950, subscription.AmountTotal)
	require.EqualValues(t, 950, subscription.AmountUsed)
	require.EqualValues(t, 950, subscription.PeriodAmount)
	require.EqualValues(t, 950, subscription.PeriodUsed)
	var order commerceschema.SubscriptionOrder
	require.NoError(t, db.Where("trade_no = ?", tradeNo).First(&order).Error)
	require.Equal(t, commerceschema.RefundStatusSuccess, order.RefundStatus)
	require.Equal(t, refundNo, order.RefundNo)

	require.NoError(t, finalizeUserRefund(userID, commerceschema.RefundOrderTypeSubscription, tradeNo, refundNo, 39.2))
	require.NoError(t, db.Where("id = ?", subscriptionID).First(&subscription).Error)
	require.EqualValues(t, 950, subscription.AmountTotal)
}

func TestSuccessfulRefundStatusCannotBeDowngraded(t *testing.T) {
	db := setupRedemptionTestDB(t)

	const (
		userID  = 5726
		tradeNo = "refund-success-is-terminal"
	)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
		UserId: userID, TradeNo: tradeNo, RefundStatus: commerceschema.RefundStatusSuccess,
		RefundProviderID: "provider-success",
	}).Error)

	require.NoError(t, setRefundStatus(userID, commerceschema.RefundOrderTypeSubscription, tradeNo, commerceschema.RefundStatusProcessing, "provider-retry", ""))

	var order commerceschema.SubscriptionOrder
	require.NoError(t, db.Where("trade_no = ?", tradeNo).First(&order).Error)
	require.Equal(t, commerceschema.RefundStatusSuccess, order.RefundStatus)
	require.Equal(t, "provider-success", order.RefundProviderID)
}
