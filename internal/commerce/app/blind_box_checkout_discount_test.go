package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuoteStripeTopUpAmountCapsBlindBoxDiscountBeforeCurrencyConversion(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8930, Username: "capped_stripe_discount", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := createBlindBoxPropTx(tx, user.Id, 8930, "九折充值卡")
		return err
	}))

	oldCNY, oldHKD, oldUSD := commercestore.StripeCNYUnitPrice, commercestore.StripeHKDUnitPrice, commercestore.StripeUSDUnitPrice
	commercestore.StripeCNYUnitPrice, commercestore.StripeHKDUnitPrice, commercestore.StripeUSDUnitPrice = 1.4, 1.5, 0.2
	t.Cleanup(func() {
		commercestore.StripeCNYUnitPrice, commercestore.StripeHKDUnitPrice, commercestore.StripeUSDUnitPrice = oldCNY, oldHKD, oldUSD
	})

	for _, test := range []struct {
		currency string
		want     string
	}{
		{currency: "cny", want: "4060.00"},
		{currency: "hkd", want: "4350.00"},
		{currency: "usd", want: "580.00"},
	} {
		t.Run(test.currency, func(t *testing.T) {
			got, err := QuoteStripeTopUpAmount(user.Id, StripePayRequest{Amount: 3000, Currency: test.currency})
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestCreatePendingTopUpOrderCapsBlindBoxDiscountByCredits(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8931, Username: "capped_topup_discount", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := createBlindBoxPropTx(tx, user.Id, 8931, "九折充值卡")
		return err
	}))
	order := &commerceschema.TopUp{
		UserId: user.Id, Amount: 3000, Money: 4500, TradeNo: "capped-topup-discount",
		Status: constant.TopUpStatusPending, CreateTime: time.Now().Unix(),
	}

	rate, err := CreatePendingTopUpOrderWithBlindBoxDiscount(order)
	require.NoError(t, err)
	require.InDelta(t, 0.10, rate, 0.000001)
	require.InDelta(t, 4350, order.Money, 0.001)
	require.Equal(t, quotaUnitsFromUSD(2900), order.PaidQuota)
}

func TestCreatePendingSubscriptionOrderCapsBlindBoxDiscount(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8932, Username: "capped_subscription_discount", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := createBlindBoxPropTx(tx, user.Id, 8932, "九折充值卡")
		return err
	}))
	plan := &commerceschema.SubscriptionPlan{
		Id: 8932, Title: "Capped subscription", PriceAmount: 3000, Currency: "CNY",
		DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, db.Create(plan).Error)
	order := &commerceschema.SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, Money: plan.PriceAmount, TradeNo: "capped-subscription-discount",
		Status: constant.TopUpStatusPending, CreateTime: time.Now().Unix(),
	}

	rate, err := CreatePendingSubscriptionOrderWithDiscounts(order, plan.PriceAmount)
	require.NoError(t, err)
	require.InDelta(t, 0.10, rate, 0.000001)
	require.InDelta(t, 2900, order.Money, 0.001)
}
