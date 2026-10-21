package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedCreditPackConversion(t *testing.T, db *gorm.DB, userID, planID int) *commerceschema.UserSubscription {
	t.Helper()
	sub, err := createCreditPackBalanceTx(db, userID, planID, 33600000, 32000000, 6000, fmt.Sprintf("conversion-pack-seed-%d", userID))
	require.NoError(t, err)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
		UserId: userID, PlanId: planID, TargetSubscriptionId: sub.Id,
		TradeNo: fmt.Sprintf("pack-conversion-order-%d", sub.Id), Money: 64, Status: constant.TopUpStatusSuccess,
		PurchaseType:      commerceschema.SubscriptionPurchaseTypeNormal,
		FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentCompleted,
		CreditPackQuota:   sub.CreditPackGrantedQuota, CreditPackPaidQuota: sub.CreditPackPaidQuota,
	}).Error)
	return sub
}

func setupCreditPackConversion(t *testing.T) (*gorm.DB, *identityschema.User, *commerceschema.UserSubscription) {
	t.Helper()
	db, user, plan, _ := setupMonthlyPassConversionTest(t, 75001, 75002, 75003, 999, 0)
	// A later catalog change must not alter paid package conversion values.
	return db, user, seedCreditPackConversion(t, db, user.Id, plan.Id)
}

func TestCreditPackConversionPartialReplayAndFullConservePaidValue(t *testing.T) {
	db, user, sub := setupCreditPackConversion(t)
	preview := BuildSubscriptionClaudeConversionPreview(&commerceschema.SubscriptionPlan{Id: sub.PlanId}, sub)
	require.True(t, preview.Eligible)
	require.Equal(t, 100, preview.MaxConversionPercent)
	require.Equal(t, 32000000, preview.PreviewQuota)
	first, err := ConvertMonthlyPassToUnifiedCredit("pack-half", user.Id, sub.Id, 50)
	require.NoError(t, err)
	require.EqualValues(t, 16800000, first.SourceQuota)
	require.Equal(t, 16000000, first.TargetQuota)
	require.False(t, first.SubscriptionEnded)
	require.Zero(t, first.BonusQuota)
	replay, err := ConvertMonthlyPassToUnifiedCredit("pack-half", user.Id, sub.Id, 50)
	require.NoError(t, err)
	require.Equal(t, first.Conversion.Id, replay.Conversion.Id)
	require.Equal(t, first.QuotaAfter, replay.QuotaAfter)
	_, err = ConvertMonthlyPassToUnifiedCredit("pack-half", user.Id, sub.Id, 25)
	require.ErrorIs(t, err, commerceschema.ErrSubscriptionClaudeConversionInvalid)
	_, err = ConvertMonthlyPassToUnifiedCredit("other-user", user.Id+1, sub.Id, 100)
	require.ErrorIs(t, err, commerceschema.ErrSubscriptionClaudeConversionNoTarget)
	_, err = ConvertMonthlyPassToUnifiedCredit("pack-half", user.Id+1, sub.Id, 50)
	require.ErrorIs(t, err, commerceschema.ErrSubscriptionClaudeConversionInvalid)
	last, err := ConvertMonthlyPassToUnifiedCredit("pack-rest", user.Id, sub.Id, 100)
	require.NoError(t, err)
	require.True(t, last.SubscriptionEnded)
	require.Equal(t, 32000100, last.QuotaAfter)
	require.Equal(t, 32000000, first.TargetQuota+last.TargetQuota)
	_, err = ConvertMonthlyPassToUnifiedCredit("pack-spent", user.Id, sub.Id, 100)
	require.Error(t, err)
	var saved commerceschema.UserSubscription
	require.NoError(t, db.First(&saved, sub.Id).Error)
	require.Equal(t, sub.AmountTotal, saved.AmountUsed)
	require.Equal(t, "cancelled", saved.Status)
	var lots []billingschema.FundingLot
	require.NoError(t, db.Where("idempotency_key IN ?", []string{monthlyPassWalletReceiptKey("pack-half", "base"), monthlyPassWalletReceiptKey("pack-rest", "base")}).Find(&lots).Error)
	require.Len(t, lots, 2)
	for _, lot := range lots {
		require.Zero(t, lot.PaidQuota)
		require.Zero(t, lot.RevenueMultiplier)
	}
}

func TestCreditPackConversionQuoteBoundaries(t *testing.T) {
	sub := &commerceschema.UserSubscription{AmountTotal: 33600000, AmountUsed: 123, CreditPackGrantedQuota: 33600000, CreditPackPaidQuota: 32000000, Status: "active", Source: "credit_pack"}
	q := calculateCreditPackConversionQuote(sub, 37)
	require.EqualValues(t, (33600000-123)*37/100, q.remainingQuota)
	require.EqualValues(t, q.remainingQuota*32000000/33600000, q.targetQuota)
	for _, percent := range []int{-1, 101} {
		require.Zero(t, calculateCreditPackConversionQuote(sub, percent).targetQuota)
	}
	sub.AmountUsed = sub.AmountTotal - 1
	require.Zero(t, calculateCreditPackConversionQuote(sub, 100).targetQuota, "do not round promotional dust into wallet value")
	sub.AmountTotal, sub.CreditPackGrantedQuota, sub.CreditPackPaidQuota, sub.AmountUsed = math.MaxInt64, math.MaxInt64, math.MaxInt64-1, 0
	require.Zero(t, calculateCreditPackConversionQuote(sub, 100).targetQuota, "database target integer cannot overflow")
	sub.AmountUsed = math.MaxInt64 - 123
	require.Equal(t, 122, calculateCreditPackConversionQuote(sub, 100).targetQuota, "exact division must not round up at large denominators")
}

func TestCreditPackConversionSelfPayloadIncludesPurchasedPack(t *testing.T) {
	_, user, sub := setupCreditPackConversion(t)
	payload, err := BuildSubscriptionSelfPayload(user.Id)
	require.NoError(t, err)
	encoded, err := json.Marshal(payload["subscriptions"])
	require.NoError(t, err)
	var items []struct {
		Subscription commerceschema.UserSubscription `json:"subscription"`
	}
	require.NoError(t, json.Unmarshal(encoded, &items))
	for _, item := range items {
		if item.Subscription.Id == sub.Id {
			require.NotNil(t, item.Subscription.ConversionPreview)
			require.True(t, item.Subscription.ConversionPreview.Eligible)
			require.Equal(t, 32000000, item.Subscription.ConversionPreview.PreviewQuota)
			return
		}
	}
	t.Fatal("purchased credit pack missing from wallet subscription payload")
}

func TestCreditPackConversionRejectsUnsafeSourcesWithoutMovingMoney(t *testing.T) {
	for _, scenario := range []string{"refund", "failed refund", "missing order", "ambiguous orders", "snapshot", "unpaid", "gift", "in flight", "projection", "receipt failure"} {
		t.Run(scenario, func(t *testing.T) {
			db, user, sub := setupCreditPackConversion(t)
			var account billingschema.BillingAccount
			require.NoError(t, db.Where("owner_type = ? AND owner_id = ?", "user_subscription", sub.Id).First(&account).Error)
			switch scenario {
			case "refund", "failed refund":
				status := commerceschema.RefundStatusProcessing
				if scenario == "failed refund" {
					status = commerceschema.RefundStatusFailed
				}
				require.NoError(t, db.Model(&commerceschema.SubscriptionOrder{}).Where("target_subscription_id = ?", sub.Id).Update("refund_status", status).Error)
			case "missing order":
				require.NoError(t, db.Where("target_subscription_id = ?", sub.Id).Delete(&commerceschema.SubscriptionOrder{}).Error)
			case "ambiguous orders":
				require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{TradeNo: "second", UserId: user.Id, TargetSubscriptionId: sub.Id}).Error)
			case "snapshot":
				require.NoError(t, db.Model(&commerceschema.SubscriptionOrder{}).Where("target_subscription_id = ?", sub.Id).Update("credit_pack_paid_quota", 1).Error)
			case "unpaid":
				require.NoError(t, db.Model(&commerceschema.SubscriptionOrder{}).Where("target_subscription_id = ?", sub.Id).Update("status", "pending").Error)
			case "gift":
				require.NoError(t, db.Model(sub).Update("source", "reset_redemption").Error)
			case "in flight":
				_, err := billingdomain.CreateReservationTx(db, billingdomain.CreateReservationParams{AccountID: account.AccountID, ReservedAmount: 100, IdempotencyKey: "open-call"})
				require.NoError(t, err)
			case "projection":
				require.NoError(t, db.Model(sub).Update("amount_used", 100).Error)
			case "receipt failure":
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail:pack-receipt", func(tx *gorm.DB) {
					if _, ok := tx.Statement.Dest.(*commerceschema.SubscriptionClaudeConversion); ok {
						tx.AddError(errors.New("receipt unavailable"))
					}
				}))
			}
			_, err := ConvertMonthlyPassToUnifiedCredit("rejected", user.Id, sub.Id, 100)
			require.Error(t, err)
			var saved identityschema.User
			require.NoError(t, db.First(&saved, user.Id).Error)
			require.Equal(t, user.ClaudeQuota, saved.ClaudeQuota)
			var count int64
			require.NoError(t, db.Model(&commerceschema.SubscriptionClaudeConversion{}).Count(&count).Error)
			require.Zero(t, count)
			var snapshot billingschema.BillingBalanceSnapshot
			require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
			if scenario == "in flight" {
				require.EqualValues(t, 100, snapshot.ReservedBalance)
			} else {
				require.EqualValues(t, 33600000, snapshot.AvailableBalance)
			}
		})
	}
}
