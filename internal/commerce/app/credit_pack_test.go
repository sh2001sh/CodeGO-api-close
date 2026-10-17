package app

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCreditPackCatalogAndCheckout(t *testing.T) {
	db := setupRedemptionTestDB(t)
	require.NoError(t, EnsureDefaultSubscriptionPlans())
	wants := []float64{67.2, 135.68, 535, 1080, 3300}
	plans := defaultSubscriptionPlans()
	require.Len(t, plans, 5)
	for i, plan := range plans {
		require.Equal(t, quotaUnitsFromUSD(wants[i]), plan.TotalAmount)
		require.Equal(t, "permanent", plan.DurationUnit)
		require.False(t, plan.GroupBuyEnabled || plan.LuckyDrawEnabled || plan.FuelEnabled)
		require.False(t, isManagedSubscriptionPlan(&plan))
	}
	var plan commerceschema.SubscriptionPlan
	require.NoError(t, db.Where("title = ?", plans[0].Title).First(&plan).Error)
	order := &commerceschema.SubscriptionOrder{UserId: 1024, PlanId: plan.Id, TradeNo: "credit-pack-checkout", Money: 64, PurchaseType: "normal", Status: constant.TopUpStatusPending}
	rate, err := CreatePendingSubscriptionOrderWithDiscounts(order, 64)
	require.NoError(t, err)
	require.Zero(t, rate)
	require.Equal(t, quotaUnitsFromUSD(67.2), order.CreditPackQuota)
	require.Equal(t, quotaUnitsFromUSD(64), order.CreditPackPaidQuota)
	require.EqualValues(t, 6000, order.CreditPackChannelShareBPS)
	bad := *order
	bad.Id, bad.TradeNo, bad.Money = 0, "credit-pack-cheap", 63
	_, err = CreatePendingSubscriptionOrderWithDiscounts(&bad, 63)
	require.Error(t, err)
	_, err = CreateUserSubscriptionFromPlanTx(db, 1024, &plan, "admin")
	require.Error(t, err)
	require.ErrorIs(t, ValidateGroupBuyPurchase(1024, plan.Id, "group_buy", 0), ErrGroupBuyPlanNotEnabled)
}

func TestCreditPackFulfillmentIsExactAndIdempotent(t *testing.T) {
	db := setupRedemptionTestDB(t)
	insertSubscriptionResetAppTestUser(t, 72031, 0)
	plan := defaultSubscriptionPlans()[0]
	require.NoError(t, db.Create(&plan).Error)
	order := &commerceschema.SubscriptionOrder{UserId: 72031, PlanId: plan.Id, TradeNo: "credit-pack-exact", Money: 64, CreditPackQuota: quotaUnitsFromUSD(67.2), CreditPackPaidQuota: quotaUnitsFromUSD(64), CreditPackChannelShareBPS: 6000, Status: constant.TopUpStatusSuccess, FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentPending}
	require.NoError(t, db.Create(order).Error)
	for i := 0; i < 2; i++ {
		require.NoError(t, FulfillPaidSubscriptionOrder(order.TradeNo))
	}
	balance, err := billingapp.GetUserClaudeWalletQuota(order.UserId)
	require.NoError(t, err)
	require.Zero(t, balance, "promotional funds must not be spendable through a wallet-only channel")
	var count int64
	require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("user_id = ?", order.UserId).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Where("id = ?", order.Id).First(order).Error)
	var sub commerceschema.UserSubscription
	require.NoError(t, db.First(&sub, order.TargetSubscriptionId).Error)
	require.Zero(t, sub.EndTime)
	require.Equal(t, "credit_pack", sub.Source)
	require.Equal(t, order.CreditPackQuota, sub.CreditPackGrantedQuota)
	require.Equal(t, order.CreditPackPaidQuota, sub.CreditPackPaidQuota)
	require.EqualValues(t, 6000, sub.CreditPackChannelShareBPS)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = (SELECT account_id FROM billing_accounts WHERE owner_type = ? AND owner_id = ? AND account_type = ?)", "user_subscription", sub.Id, "subscription").First(&snapshot).Error)
	require.Equal(t, order.CreditPackQuota, snapshot.AvailableBalance)
	var topups int64
	require.NoError(t, db.Model(&commerceschema.TopUp{}).Where("trade_no = ?", order.TradeNo).Count(&topups).Error)
	require.Zero(t, topups)
	// A halfway used bonus package refunds half of the cash paid, including bonus consumption.
	gross, _, _ := refundMoney(64, sub.AmountTotal/2, sub.AmountTotal)
	require.Equal(t, 32.0, gross)
}

func TestCreditPackCheckoutIgnoresLegacyTierAndPreservesExistingRefresh(t *testing.T) {
	db := setupRedemptionTestDB(t)
	insertSubscriptionResetAppTestUser(t, 72061, 0)
	insertSubscriptionResetAppTestUser(t, 72062, 72061)
	legacy := insertSubscriptionResetAppTestPlan(t, 72061, 30, 1000)
	legacy.PriceAmount = 3000
	require.NoError(t, db.Save(legacy).Error)
	sub := commerceschema.UserSubscription{UserId: 72061, PlanId: legacy.Id, AmountTotal: 1000, AmountUsed: 100, StartTime: 1, EndTime: time.Now().Add(time.Hour).Unix(), Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	account := commerceschema.SubscriptionResetOpportunityAccount{UserId: 72061, EarnedTotal: 3, AvailableTotal: 2, UsedTotal: 1}
	require.NoError(t, db.Create(&account).Error)
	pack := defaultSubscriptionPlans()[0]
	require.NoError(t, db.Create(&pack).Error)
	preview, err := ResolveSubscriptionPurchasePreview(72061, &pack)
	require.NoError(t, err)
	require.Equal(t, commerceschema.SubscriptionPurchaseActionSubscribe, preview.Action)
	require.Equal(t, 64.0, preview.AmountDue)
	require.Nil(t, preview.CurrentSubscription)
	require.NoError(t, AwardReferralSubscriptionResetOpportunityTx(db, 72062, "month_card", "subscription_order", "retired-invitation"))
	var saved commerceschema.SubscriptionResetOpportunityAccount
	require.NoError(t, db.Where("user_id = ?", 72061).First(&saved).Error)
	require.Equal(t, account, saved)
}

func TestCreditPackCannotConsumeLegacyRefresh(t *testing.T) {
	db := setupRedemptionTestDB(t)
	insertSubscriptionResetAppTestUser(t, 72071, 0)
	pack := defaultSubscriptionPlans()[0]
	require.NoError(t, db.Create(&pack).Error)
	// Even an erroneously imported subscription must not expose a permanent pack to refreshes.
	sub := commerceschema.UserSubscription{UserId: 72071, PlanId: pack.Id, AmountTotal: pack.TotalAmount, AmountUsed: 100, StartTime: 1, EndTime: time.Now().Add(time.Hour).Unix(), Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	account := commerceschema.SubscriptionResetOpportunityAccount{UserId: 72071, EarnedTotal: 2, AvailableTotal: 2}
	require.NoError(t, db.Create(&account).Error)
	_, err := UseUserSubscriptionResetOpportunity(72071)
	require.ErrorIs(t, err, commerceschema.ErrSubscriptionResetOpportunityNoActiveSub)
	var saved commerceschema.SubscriptionResetOpportunityAccount
	require.NoError(t, db.Where("user_id = ?", 72071).First(&saved).Error)
	require.Equal(t, account, saved)
	var current commerceschema.UserSubscription
	require.NoError(t, db.First(&current, sub.Id).Error)
	require.Equal(t, sub.AmountUsed, current.AmountUsed)
}

func TestCreditPackSeedPreservesLegacyAndDisabledOffers(t *testing.T) {
	db := setupRedemptionTestDB(t)
	plan := insertSubscriptionResetAppTestPlan(t, 72041, 30, 50_000)
	sub := commerceschema.UserSubscription{UserId: 72041, PlanId: plan.Id, AmountTotal: 50_000, AmountUsed: 12_000, StartTime: 1, EndTime: time.Now().Add(time.Hour).Unix(), Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, EnsureDefaultSubscriptionPlans())
	var saved commerceschema.UserSubscription
	require.NoError(t, db.First(&saved, sub.Id).Error)
	require.Equal(t, sub.AmountTotal, saved.AmountTotal)
	require.Equal(t, sub.AmountUsed, saved.AmountUsed)
	require.Equal(t, sub.EndTime, saved.EndTime)
	var offer commerceschema.SubscriptionPlan
	require.NoError(t, db.Where("plan_type = ?", "credit_pack").First(&offer).Error)
	require.NoError(t, db.Model(&offer).Update("enabled", false).Error)
	require.NoError(t, EnsureDefaultSubscriptionPlans())
	require.NoError(t, db.First(&offer, offer.Id).Error)
	require.False(t, offer.Enabled)
}

func TestCreditPackConversionUsesActualLegacyPayment(t *testing.T) {
	db := setupRedemptionTestDB(t)
	plan := insertSubscriptionResetAppTestPlan(t, 72051, 7, 1000)
	sub := &commerceschema.UserSubscription{Id: 72051, UserId: 72051, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 400, StartTime: 1, EndTime: time.Now().Add(time.Hour).Unix(), Status: "active"}
	order := commerceschema.SubscriptionOrder{TargetSubscriptionId: sub.Id, UserId: sub.UserId, TradeNo: "old-discounted", Money: 32, Status: constant.TopUpStatusSuccess}
	require.NoError(t, db.Create(&order).Error)
	priced, err := legacyConversionPaidPlanTx(db, plan, sub)
	require.NoError(t, err)
	quote := calculateMonthlyPassConversionQuote(priced, sub, time.Now().Unix(), 60)
	require.Equal(t, quotaUnitsFromUSD(19.2), int64(quote.baseQuota))
	require.Equal(t, quotaUnitsFromUSD(19.2), int64(quote.targetQuota))
	require.Equal(t, 50.0, plan.PriceAmount)
	// Retired invitation rewards cannot create new refresh balances.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return AwardReferralSubscriptionResetOpportunityTx(tx, sub.UserId, "month_card", "order", "new")
	}))
	var count int64
	require.NoError(t, db.Model(&commerceschema.SubscriptionResetOpportunityLedger{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestCreditPackFundingSeparatesLegacyAndPreservesNativeSettlement(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)
	insertSubscriptionResetAppTestUser(t, 72101, 0)
	plan := insertSubscriptionResetAppTestPlan(t, 72102, 0, 10_000)
	legacy := commerceschema.UserSubscription{UserId: 72101, PlanId: plan.Id, AmountTotal: 10_000, Status: "active", EndTime: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, db.Create(&legacy).Error)
	pack, err := createCreditPackBalanceTx(db, legacy.UserId, plan.Id, 1_100, 1_000, 6000, "test-isolation-credit")
	require.NoError(t, err)
	oldResult, err := PreConsumeUserSubscription("legacy-isolated-request", legacy.UserId, "gpt-5", 100)
	require.NoError(t, err)
	require.Equal(t, legacy.Id, oldResult.UserSubscriptionId)
	require.Zero(t, oldResult.CreditPackGrantedQuota)
	result, err := PreConsumeCreditPack("new-isolated-request", legacy.UserId, "gpt-5", 200)
	require.NoError(t, err)
	require.Equal(t, pack.Id, result.UserSubscriptionId)
	require.EqualValues(t, 1100, result.CreditPackGrantedQuota)
	require.EqualValues(t, 1000, result.CreditPackPaidQuota)
	require.EqualValues(t, 6000, result.CreditPackChannelShareBPS)
	replay, err := PreConsumeCreditPack("new-isolated-request", legacy.UserId, "gpt-5", 200)
	require.NoError(t, err)
	require.Equal(t, result.ReservationID, replay.ReservationID)
	require.Equal(t, result.AccountID, replay.AccountID)
	_, err = PreConsumeUserSubscription("new-isolated-request", legacy.UserId, "gpt-5", 200)
	require.Error(t, err, "replay must not switch the funding source")
	_, err = PreConsumeCreditPack("new-isolated-request", legacy.UserId+1, "gpt-5", 200)
	require.Error(t, err, "request replay must retain ownership")
	require.NoError(t, ReserveAdditionalSubscriptionQuota("new-isolated-request", pack.Id, "gpt-5", 100))
	require.NoError(t, SettleSubscriptionReservation("new-isolated-request", pack.Id, "gpt-5", 250))
	require.NoError(t, SettleSubscriptionReservation("new-isolated-request", pack.Id, "gpt-5", 250))
	_, err = PreConsumeCreditPack("new-failed-request", legacy.UserId, "gpt-5", 100)
	require.NoError(t, err)
	require.NoError(t, RefundSubscriptionPreConsume("new-failed-request"))
	require.NoError(t, RefundSubscriptionPreConsume("new-failed-request"))
	require.NoError(t, db.First(pack, pack.Id).Error)
	require.EqualValues(t, 250, pack.AmountUsed)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", result.AccountID).First(&snapshot).Error)
	require.EqualValues(t, 850, snapshot.AvailableBalance)
	require.Zero(t, snapshot.ReservedBalance)
	require.EqualValues(t, 250, snapshot.ConsumedTotal)
	var savedLegacy commerceschema.UserSubscription
	require.NoError(t, db.First(&savedLegacy, legacy.Id).Error)
	require.EqualValues(t, 100, savedLegacy.AmountUsed)
	_, err = PreConsumeCreditPack("new-overdraw", legacy.UserId, "gpt-5", 851)
	require.Error(t, err)
}

func TestCreditPackPermanentSnapshotSurvivesLegacyCatalogAndMaintenance(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)
	insertSubscriptionResetAppTestUser(t, 72111, 0)
	plan := insertSubscriptionResetAppTestPlan(t, 72112, 0, 1000)
	plan.QuotaResetPeriod, plan.PeriodAmount = commerceschema.SubscriptionResetDaily, 1
	require.NoError(t, db.Save(plan).Error)
	pack, err := createCreditPackBalanceTx(db, 72111, plan.Id, 1_000, 1_000, 6000, "test-permanent-credit")
	require.NoError(t, err)
	_, err = PreConsumeCreditPack("permanent-spend", 72111, "gpt-5", 100)
	require.NoError(t, err, "converted balances must ignore the old periodic cap")
	require.NoError(t, SettleSubscriptionReservation("permanent-spend", pack.Id, "gpt-5", 100))
	require.NoError(t, db.Model(pack).Update("next_reset_time", 1).Error)
	count, err := ResetDueSubscriptions(100)
	require.NoError(t, err)
	require.Zero(t, count)
	count, err = ExpireDueSubscriptions(100)
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, db.First(pack, pack.Id).Error)
	require.EqualValues(t, 100, pack.AmountUsed)
	require.Zero(t, pack.EndTime)
	require.Equal(t, "active", pack.Status)
	active, err := GetAllActiveUserSubscriptions(pack.UserId)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, "credit_pack", active[0].Plan.PlanType)
	require.Equal(t, "permanent", active[0].Plan.DurationUnit)
	require.Equal(t, "never", active[0].Plan.QuotaResetPeriod)
}

func TestCreditPackRefundConsumesOnlyItsOwnLedgerAndRetainsSnapshots(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)
	insertSubscriptionResetAppTestUser(t, 72121, 0)
	plan := defaultSubscriptionPlans()[4]
	require.NoError(t, db.Create(&plan).Error)
	order := &commerceschema.SubscriptionOrder{UserId: 72121, PlanId: plan.Id, TradeNo: "refundable-credit-pack", Money: 100, CreditPackQuota: 1_100, CreditPackPaidQuota: 1_000, CreditPackChannelShareBPS: 6000, Status: constant.TopUpStatusSuccess, PaymentProvider: commerceschema.PaymentProviderEpay, PaymentMethod: "alipay", FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentPending, ProviderPayload: `{"trade_no":"provider-pack"}`}
	require.NoError(t, db.Create(order).Error)
	require.NoError(t, FulfillPaidSubscriptionOrder(order.TradeNo))
	require.NoError(t, db.First(order, order.Id).Error)
	result, err := PreConsumeCreditPack("refund-pack-spend", order.UserId, "gpt-5", 400)
	require.NoError(t, err)
	pending := refundableSubscriptionWithDB(db, order.UserId, order)
	require.False(t, pending.Refundable)
	require.Contains(t, pending.UnavailableReason, "待结算")
	require.NoError(t, SettleSubscriptionReservation("refund-pack-spend", result.UserSubscriptionId, "gpt-5", 400))
	item := refundableSubscriptionWithDB(db, order.UserId, order)
	require.True(t, item.Refundable)
	require.EqualValues(t, 700, item.RemainingQuota)
	require.Equal(t, 63.64, item.GrossRefund)
	order.RefundQuota, order.RefundStatus, order.RefundNo, order.RefundAmount = item.RemainingQuota, commerceschema.RefundStatusProcessing, "refund-pack-final", item.RefundAmount
	require.NoError(t, db.Save(order).Error)
	var refundablePack commerceschema.UserSubscription
	require.NoError(t, db.First(&refundablePack, result.UserSubscriptionId).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reserveSubscriptionRefundTx(tx, &refundablePack, item.RemainingQuota, order.RefundNo)
	}))
	require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("id = ?", result.UserSubscriptionId).Update("status", "refund_pending").Error)
	_, err = PreConsumeCreditPack("refund-pack-blocked", order.UserId, "gpt-5", 1)
	require.Error(t, err)
	for i := 0; i < 2; i++ {
		require.NoError(t, finalizeUserRefund(order.UserId, commerceschema.RefundOrderTypeSubscription, order.TradeNo, "refund-pack-final", item.RefundAmount))
	}
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", result.AccountID).First(&snapshot).Error)
	require.Zero(t, snapshot.AvailableBalance)
	require.Zero(t, snapshot.ReservedBalance)
	var pack commerceschema.UserSubscription
	require.NoError(t, db.First(&pack, result.UserSubscriptionId).Error)
	require.Equal(t, commerceschema.SubscriptionStatusSettledCancelled, pack.Status)
	require.EqualValues(t, 1_100, pack.CreditPackGrantedQuota)
	require.EqualValues(t, 1_000, pack.CreditPackPaidQuota)
	balance, err := billingapp.GetUserClaudeWalletQuota(order.UserId)
	require.NoError(t, err)
	require.Zero(t, balance)
}

func TestCreditPackConversionMovesRemainingValueWithoutWalletDoubleSpend(t *testing.T) {
	db, user, _, old := setupMonthlyPassConversionTest(t, 72131, 72132, 72133, 64, 50)
	ensureSubscriptionPreConsumeRecordSchema(t)
	conversion, err := ConvertMonthlyPassToUnifiedCredit("convert-new-source", user.Id, old.Id, 50)
	require.NoError(t, err)
	require.Zero(t, conversion.TargetCreditPackSubscriptionId)
	require.Equal(t, "wallet", conversion.TargetType)
	require.Equal(t, quotaUnitsFromUSD(32), int64(conversion.BaseQuota))
	require.Zero(t, conversion.BonusQuota)
	_, err = PreConsumeUserSubscription("converted-no-old", user.Id, "gpt-5", 1)
	require.Error(t, err)
	_, err = PreConsumeCreditPack("converted-use-new", user.Id, "gpt-5", 100)
	require.Error(t, err, "conversion must not create a credit-pack balance")
	replay, err := ConvertMonthlyPassToUnifiedCredit("convert-new-source", user.Id, old.Id, 50)
	require.NoError(t, err)
	require.Equal(t, conversion.TargetQuota, replay.TargetQuota)
	require.Zero(t, replay.TargetCreditPackSubscriptionId)
	var count int64
	require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("credit_pack_granted_quota > 0 AND user_id = ?", user.Id).Count(&count).Error)
	require.Zero(t, count)
	var wallet billingschema.BillingAccount
	query := db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user", user.Id, "claude_wallet").Find(&wallet)
	require.NoError(t, query.Error)
	require.EqualValues(t, 1, query.RowsAffected)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", wallet.AccountID).First(&snapshot).Error)
	require.EqualValues(t, user.ClaudeQuota+conversion.TargetQuota, snapshot.AvailableBalance)
}

func TestCreditPackPurchaseThroughRealBillingSessionUsesOneTimesLedgerDebit(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)
	insertSubscriptionResetAppTestUser(t, 72141, 0)
	plan := defaultSubscriptionPlans()[0]
	require.NoError(t, db.Create(&plan).Error)
	order := &commerceschema.SubscriptionOrder{UserId: 72141, PlanId: plan.Id, TradeNo: "real-billing-pack", Money: 64, PurchaseType: "normal", Status: constant.TopUpStatusPending}
	_, err := CreatePendingSubscriptionOrderWithDiscounts(order, 64)
	require.NoError(t, err)
	require.NoError(t, db.Model(order).Update("status", constant.TopUpStatusSuccess).Error)
	require.NoError(t, FulfillPaidSubscriptionOrder(order.TradeNo))
	require.NoError(t, db.First(order, order.Id).Error)
	policy := gatewaystore.SubscriptionGroupPolicy2JSONString()
	require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(`{"credit_pack_integration":{"enabled":true,"multiplier":10}}`))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(policy)) })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: order.UserId, OriginModelName: "gpt-5", RequestId: "real-pack-relay", UsingGroup: "credit_pack_integration", IsPlayground: true, ForcePreConsume: true,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeOfficial, ChannelId: 123},
		UserSetting: dto.UserSetting{BillingPreference: "subscription_only"},
		PriceData:   types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	session, apiErr := billingapp.NewBillingSession(ctx, info, 1_000)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, 1_000, session.GetPreConsumedQuota(), "new pack must ignore the ten-times legacy policy")
	require.Equal(t, 1.0, info.SubscriptionQuotaScale)
	require.Equal(t, order.CreditPackQuota, info.CreditPackGrantedQuota)
	require.NoError(t, session.Settle(800))
	require.NoError(t, session.Settle(800))
	var sub commerceschema.UserSubscription
	require.NoError(t, db.First(&sub, order.TargetSubscriptionId).Error)
	require.EqualValues(t, 800, sub.AmountUsed)
	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user_subscription", sub.Id, "subscription").First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	require.Equal(t, sub.AmountTotal-800, snapshot.AvailableBalance)
	require.Zero(t, snapshot.ReservedBalance)
	var record commerceschema.SubscriptionPreConsumeRecord
	require.NoError(t, db.Where("user_subscription_id = ?", sub.Id).First(&record).Error)
	require.Contains(t, record.RequestId, "cp:")
	require.NotEqual(t, info.RequestId, record.RequestId)
	var walletAccounts int64
	require.NoError(t, db.Model(&billingschema.BillingAccount{}).Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user", order.UserId, "claude_wallet").Count(&walletAccounts).Error)
	require.Zero(t, walletAccounts)
}

func TestCreditPackCheckoutRejectsBonusOutsideApprovedBand(t *testing.T) {
	db := setupRedemptionTestDB(t)
	for index, granted := range []float64{104.999998, 105, 110, 110.000002} {
		plan := defaultSubscriptionPlans()[0]
		plan.Title, plan.PriceAmount, plan.TotalAmount = "bonus-band", 100, quotaUnitsFromUSD(granted)
		plan.Id = 72200 + index
		require.NoError(t, db.Create(&plan).Error)
		order := &commerceschema.SubscriptionOrder{UserId: 72151, PlanId: plan.Id, TradeNo: plan.Title + string(rune('a'+index)), Money: 100, PurchaseType: "normal", Status: constant.TopUpStatusPending}
		_, err := CreatePendingSubscriptionOrderWithDiscounts(order, 100)
		if index == 1 || index == 2 {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "5% and 10%")
		}
	}
}

func TestCreditPackBillingSelectsPackBeforeLegacyTenfoldExceedsKeyBalance(t *testing.T) {
	db := setupRedemptionTestDB(t)
	// The onefold pack takes priority over legacy subscription pricing, even
	// when the API key cannot cover the legacy tenfold reservation.
	wasSQLite := platformdb.UsingSQLite
	platformdb.UsingSQLite = false
	t.Cleanup(func() { platformdb.UsingSQLite = wasSQLite })
	ensureSubscriptionPreConsumeRecordSchema(t)
	require.NoError(t, db.AutoMigrate(&identityschema.Token{}))
	insertSubscriptionResetAppTestUser(t, 72161, 0)
	plan := insertSubscriptionResetAppTestPlan(t, 72162, 0, 10_000)
	old := &commerceschema.UserSubscription{UserId: 72161, PlanId: plan.Id, AmountTotal: 10_000, Status: "active", EndTime: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, db.Create(old).Error)
	pack, err := createCreditPackBalanceTx(db, old.UserId, plan.Id, 1100, 1000, 6000, "key-fallback-pack")
	require.NoError(t, err)
	token := &identityschema.Token{UserId: old.UserId, Key: "test-credit-pack-fallback", RemainQuota: 500, ExpiredTime: -1, Status: constant.TokenStatusEnabled}
	require.NoError(t, db.Create(token).Error)
	policy := gatewaystore.SubscriptionGroupPolicy2JSONString()
	require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(`{"credit_pack_key_fallback":{"enabled":true,"multiplier":10}}`))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(policy)) })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: old.UserId, TokenId: token.Id, TokenKey: token.Key, OriginModelName: "gpt-5", RequestId: "key-fallback-request", UsingGroup: "credit_pack_key_fallback", ForcePreConsume: true,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelScope: gatewayschema.ChannelScopeOfficial, ChannelId: 123}, UserSetting: dto.UserSetting{BillingPreference: "subscription_only"},
		PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	session, apiErr := billingapp.NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, pack.Id, info.SubscriptionId)
	require.Equal(t, 100, session.GetPreConsumedQuota())
	require.NoError(t, session.Settle(80))
	require.NoError(t, db.First(token, token.Id).Error)
	require.Equal(t, 420, token.RemainQuota)
	require.Equal(t, 80, token.UsedQuota)
	require.NoError(t, db.First(old, old.Id).Error)
	require.Zero(t, old.AmountUsed)
	var legacyAttempts int64
	require.NoError(t, db.Model(&commerceschema.SubscriptionPreConsumeRecord{}).Where("request_id = ?", info.RequestId).Count(&legacyAttempts).Error)
	require.Zero(t, legacyAttempts, "the selected pack must not reserve the legacy subscription")
	require.NoError(t, db.First(pack, pack.Id).Error)
	require.EqualValues(t, 80, pack.AmountUsed)
}

func TestCreditPackConversionUsesRemainingQuotaForHistoricalUpgradePayments(t *testing.T) {
	db, user, plan, sub := setupMonthlyPassConversionTest(t, 72171, 72172, 72173, 299, 50)
	for index, money := range []float64{64, 20} {
		order := &commerceschema.SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, TargetSubscriptionId: sub.Id, TradeNo: "historical-upgrade-" + string(rune('a'+index)), Money: money, Status: constant.TopUpStatusSuccess}
		require.NoError(t, db.Create(order).Error)
	}
	preview := BuildSubscriptionClaudeConversionPreview(plan, sub)
	require.True(t, preview.Eligible)
	require.EqualValues(t, 299, preview.PlanPriceAmount)
	require.EqualValues(t, quotaUnitsFromUSD(149.5), preview.BaseQuota)
	result, err := ConvertMonthlyPassToUnifiedCredit("historical-upgrade-conversion", user.Id, sub.Id, 50)
	require.NoError(t, err)
	require.EqualValues(t, preview.BaseQuota, result.TargetQuota)
	replayed, err := ConvertMonthlyPassToUnifiedCredit("historical-upgrade-conversion", user.Id, sub.Id, 50)
	require.NoError(t, err)
	require.Equal(t, result.Conversion.Id, replayed.Conversion.Id)
	_, err = ConvertMonthlyPassToUnifiedCredit("historical-upgrade-conversion-new-key", user.Id, sub.Id, 50)
	require.Error(t, err)
	var saved commerceschema.UserSubscription
	require.NoError(t, db.First(&saved, sub.Id).Error)
	require.Equal(t, saved.AmountTotal, saved.AmountUsed)
	require.Equal(t, "cancelled", saved.Status)
	var savedUser identityschema.User
	require.NoError(t, db.First(&savedUser, user.Id).Error)
	require.EqualValues(t, 100+preview.BaseQuota, savedUser.ClaudeQuota)
	var count int64
	require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("user_id = ? AND credit_pack_granted_quota > 0", user.Id).Count(&count).Error)
	require.Zero(t, count)
}
