package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	luckysettings "github.com/sh2001sh/new-api/internal/commerce/luckysettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func prepareDailyLuckyNumberTestDB(t *testing.T) {
	t.Helper()
	db := setupRedemptionTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&commerceschema.SubscriptionLuckyNumber{},
		&commerceschema.SubscriptionLuckyDraw{},
		&commerceschema.SubscriptionLuckyReward{},
		&commerceschema.SubscriptionBlindBoxBenefitCycle{},
	))
}

func TestLuckyMatchDigitsReturnsOnlyTheHighestSuffixMatch(t *testing.T) {
	tests := []struct {
		name          string
		luckySuffix   string
		winningNumber string
		matched       int
	}{
		{name: "no match", luckySuffix: "7316", winningNumber: "5800", matched: 0},
		{name: "one digit with leading zero", luckySuffix: "7316", winningNumber: "0006", matched: 1},
		{name: "two digits", luckySuffix: "7316", winningNumber: "5816", matched: 2},
		{name: "three digits", luckySuffix: "7316", winningNumber: "1316", matched: 3},
		{name: "four digits", luckySuffix: "7316", winningNumber: "7316", matched: 4},
		{name: "invalid length", luckySuffix: "316", winningNumber: "0316", matched: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.matched, luckyMatchDigits(test.luckySuffix, test.winningNumber))
		})
	}
}

func TestLuckyNumberAllocationIsStableAndReusableBySuffix(t *testing.T) {
	prepareDailyLuckyNumberTestDB(t)
	db := platformDBForDailyLuckyTest(t)

	user := &identityschema.User{Id: 9901, Username: "lucky-number-user", Status: constant.UserStatusEnabled}
	plan := &commerceschema.SubscriptionPlan{
		Id:               9902,
		Title:            "Standard monthly",
		PlanType:         commerceschema.SubscriptionPlanTypeMonthly,
		MembershipTier:   commerceschema.SubscriptionMembershipTierStandard,
		LuckyDrawEnabled: true,
	}
	subscription := &commerceschema.UserSubscription{
		Id:        9903,
		UserId:    user.Id,
		PlanId:    plan.Id,
		StartTime: time.Now().Add(-time.Hour).Unix(),
		EndTime:   time.Now().Add(time.Hour).Unix(),
		Status:    "active",
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(plan).Error)
	require.NoError(t, db.Create(subscription).Error)

	first, err := ensureSubscriptionLuckyNumberTx(db, subscription, plan)
	require.NoError(t, err)
	require.NotNil(t, first)
	second, err := ensureSubscriptionLuckyNumberTx(db, subscription, plan)
	require.NoError(t, err)
	require.Equal(t, first.Id, second.Id)
	require.Equal(t, first.CardCode, second.CardCode)
	require.Len(t, first.LuckySuffix, 4)

	var count int64
	require.NoError(t, db.Model(&commerceschema.SubscriptionLuckyNumber{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestDailyLuckyDrawCreationIsIdempotent(t *testing.T) {
	prepareDailyLuckyNumberTestDB(t)
	db := platformDBForDailyLuckyTest(t)
	setting := luckysettings.Get()
	now := time.Now()

	user := &identityschema.User{Id: 9911, Username: "lucky-draw-user", Status: constant.UserStatusEnabled}
	plan := &commerceschema.SubscriptionPlan{
		Id:               9912,
		Title:            "Pro monthly",
		PlanType:         commerceschema.SubscriptionPlanTypeMonthly,
		MembershipTier:   commerceschema.SubscriptionMembershipTierPro,
		LuckyDrawEnabled: true,
	}
	subscription := &commerceschema.UserSubscription{
		Id:          9913,
		UserId:      user.Id,
		PlanId:      plan.Id,
		AmountTotal: int64(platformruntime.QuotaPerUnit * 100),
		StartTime:   now.Add(-time.Hour).Unix(),
		EndTime:     now.Add(time.Hour).Unix(),
		Status:      "active",
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(plan).Error)
	require.NoError(t, db.Create(subscription).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionLuckyNumber{
		Id:                 9914,
		UserSubscriptionId: subscription.Id,
		UserId:             user.Id,
		CardCode:           "CG-ABCDEF-7316",
		LuckySuffix:        "7316",
	}).Error)

	first, err := createDailyLuckyDraw("2099-01-01", now, setting)
	require.NoError(t, err)
	require.NotNil(t, first)
	second, err := createDailyLuckyDraw("2099-01-01", now, setting)
	require.NoError(t, err)
	require.Equal(t, first.Id, second.Id)
	require.Equal(t, first.WinningNumber, second.WinningNumber)

	var drawCount int64
	require.NoError(t, db.Model(&commerceschema.SubscriptionLuckyDraw{}).Count(&drawCount).Error)
	require.Equal(t, int64(1), drawCount)
}

func TestDailyLuckyRewardSettlementWritesLedgerOnce(t *testing.T) {
	prepareDailyLuckyNumberTestDB(t)
	db := platformDBForDailyLuckyTest(t)
	now := time.Now().Unix()
	user := &identityschema.User{Id: 9921, Username: "lucky-reward-user", Status: constant.UserStatusEnabled}
	plan := &commerceschema.SubscriptionPlan{Id: 9922, Title: "Lite monthly", PlanType: commerceschema.SubscriptionPlanTypeMonthly}
	subscription := &commerceschema.UserSubscription{
		Id:          9923,
		UserId:      user.Id,
		PlanId:      plan.Id,
		AmountTotal: int64(platformruntime.QuotaPerUnit * 100),
		AmountUsed:  int64(platformruntime.QuotaPerUnit * 20),
		StartTime:   now - 3600,
		EndTime:     now + 3600,
		Status:      "active",
	}
	draw := &commerceschema.SubscriptionLuckyDraw{Id: 9924, DrawDate: "2099-01-02", WinningNumber: "7316", Status: commerceschema.SubscriptionLuckyDrawStatusSettling}
	reward := &commerceschema.SubscriptionLuckyReward{
		Id:                 9925,
		DrawId:             draw.Id,
		UserSubscriptionId: subscription.Id,
		UserId:             user.Id,
		LuckyNumber:        "7316",
		MatchedDigits:      4,
		FinalRewardQuota:   int64(platformruntime.QuotaPerUnit * 10),
		CreditStatus:       commerceschema.SubscriptionLuckyRewardCreditPending,
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(plan).Error)
	require.NoError(t, db.Create(subscription).Error)
	require.NoError(t, db.Create(draw).Error)
	require.NoError(t, db.Create(reward).Error)

	require.NoError(t, settleDailyLuckyReward(reward.Id))
	require.NoError(t, settleDailyLuckyReward(reward.Id))
	require.NoError(t, settleDailyLuckyDraw(draw.Id))
	require.NoError(t, settleDailyLuckyDraw(draw.Id))

	var savedSubscription commerceschema.UserSubscription
	require.NoError(t, db.First(&savedSubscription, subscription.Id).Error)
	require.Equal(t, subscription.AmountTotal, savedSubscription.AmountTotal)
	var savedUser identityschema.User
	require.NoError(t, db.First(&savedUser, user.Id).Error)
	require.Equal(t, int(reward.FinalRewardQuota), savedUser.Quota)
	snapshot := loadCommerceBillingSnapshot(t, user.Id, "wallet")
	require.Equal(t, reward.FinalRewardQuota, snapshot.AvailableBalance)
	var savedReward commerceschema.SubscriptionLuckyReward
	require.NoError(t, db.First(&savedReward, reward.Id).Error)
	require.Equal(t, commerceschema.SubscriptionLuckyRewardCreditCredited, savedReward.CreditStatus)
	var savedDraw commerceschema.SubscriptionLuckyDraw
	require.NoError(t, db.First(&savedDraw, draw.Id).Error)
	require.Equal(t, commerceschema.SubscriptionLuckyDrawStatusCompleted, savedDraw.Status)

	var ledgerCount int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("reason_code = ?", "subscription_lucky_draw").Count(&ledgerCount).Error)
	require.Equal(t, int64(1), ledgerCount)
	var logs []auditschema.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", user.Id, auditschema.LogTypeTopup).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Content, "每日幸运号中奖到账")
}

func TestSubscriptionPurchaseDoesNotGrantBlindBoxBenefits(t *testing.T) {
	prepareDailyLuckyNumberTestDB(t)
	db := platformDBForDailyLuckyTest(t)
	user := &identityschema.User{Id: 9931, Username: "lucky-benefit-user", Status: constant.UserStatusEnabled}
	plan := &commerceschema.SubscriptionPlan{
		Id:                   9932,
		Title:                "Lite monthly",
		PlanType:             commerceschema.SubscriptionPlanTypeMonthly,
		DurationUnit:         commerceschema.SubscriptionDurationMonth,
		DurationValue:        1,
		PriceAmount:          49,
		TotalAmount:          quotaUnitsFromUSD(300),
		MembershipTier:       commerceschema.SubscriptionMembershipTierLite,
		BlindBoxBenefitCount: 1,
	}
	higherPlan := &commerceschema.SubscriptionPlan{
		Id:                   9933,
		Title:                "Standard monthly",
		PlanType:             commerceschema.SubscriptionPlanTypeMonthly,
		DurationUnit:         commerceschema.SubscriptionDurationMonth,
		DurationValue:        1,
		PriceAmount:          89,
		TotalAmount:          quotaUnitsFromUSD(620),
		MembershipTier:       commerceschema.SubscriptionMembershipTierStandard,
		BlindBoxBenefitCount: 2,
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(plan).Error)
	require.NoError(t, db.Create(higherPlan).Error)

	subscription, preview, err := ApplySubscriptionPurchaseTx(db, user.Id, plan, "order")
	require.NoError(t, err)
	require.NotNil(t, subscription)
	require.Equal(t, commerceschema.SubscriptionPurchaseActionSubscribe, preview.Action)
	require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("id = ?", subscription.Id).Update("amount_used", plan.TotalAmount/2).Error)

	_, preview, err = ApplySubscriptionPurchaseTx(db, user.Id, plan, "order")
	require.NoError(t, err)
	require.Equal(t, commerceschema.SubscriptionPurchaseActionRenew, preview.Action)
	_, preview, err = ApplySubscriptionPurchaseTx(db, user.Id, higherPlan, "order")
	require.NoError(t, err)
	require.Equal(t, commerceschema.SubscriptionPurchaseActionUpgrade, preview.Action)

	var benefits int64
	require.NoError(t, db.Model(&commerceschema.SubscriptionBlindBoxBenefitCycle{}).Count(&benefits).Error)
	require.Zero(t, benefits)
	var orders int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxOrder{}).Where("source = ?", commerceschema.BlindBoxOrderSourceSubscriptionBenefit).Count(&orders).Error)
	require.Zero(t, orders)
}

func platformDBForDailyLuckyTest(t *testing.T) *gorm.DB {
	t.Helper()
	return platformdb.DB
}
