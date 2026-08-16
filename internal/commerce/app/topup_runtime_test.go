package app

import (
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestCompleteTopUpByTradeNo_ConsumesReservedBlindBoxProp(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{
		Id:       8910,
		Username: "blind_box_prop_topup_user",
		Status:   constant.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	var prop *commerceschema.BlindBoxProp
	err := db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		prop, txErr = createBlindBoxPropTx(tx, user.Id, 2, "充值九折卡")
		return txErr
	})
	require.NoError(t, err)
	require.NotNil(t, prop)

	topUp := &commerceschema.TopUp{
		UserId:     user.Id,
		Amount:     2,
		Money:      1,
		TradeNo:    "blind-box-topup-discount-order",
		Status:     constant.TopUpStatusPending,
		CreateTime: time.Now().Unix(),
	}
	appliedRate, err := CreatePendingTopUpOrderWithBlindBoxDiscount(topUp)
	require.NoError(t, err)
	assert.InDelta(t, 0.10, appliedRate, 0.0001)
	assert.Equal(t, 0.9, topUp.Money)

	var reserved commerceschema.BlindBoxProp
	require.NoError(t, db.Where("id = ?", prop.Id).First(&reserved).Error)
	assert.Equal(t, commerceschema.BlindBoxPropStatusReserved, reserved.Status)
	assert.Equal(t, commerceschema.BlindBoxPropOrderTypeTopup, reserved.ReservedOrderType)
	assert.Equal(t, topUp.TradeNo, reserved.ReservedOrderTradeNo)

	completedTopUp, quotaToAdd, err := CompleteTopUpByTradeNo(topUp.TradeNo, "", commerceschema.PaymentMethodStripe, "", "")
	require.NoError(t, err)
	require.NotNil(t, completedTopUp)
	assert.Equal(t, constant.TopUpStatusSuccess, completedTopUp.Status)
	assert.Positive(t, quotaToAdd)

	var used commerceschema.BlindBoxProp
	require.NoError(t, db.Where("id = ?", prop.Id).First(&used).Error)
	assert.Equal(t, commerceschema.BlindBoxPropStatusUsed, used.Status)
	assert.NotZero(t, used.UsedAt)
	assert.Empty(t, used.ReservedOrderType)
	assert.Empty(t, used.ReservedOrderTradeNo)

	var savedUser identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	assert.Greater(t, savedUser.ClaudeQuota, 0)
}

func TestRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{
		Id:       8911,
		Username: "payment_guard_user",
		Status:   constant.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	topUp := &commerceschema.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		TradeNo:         "waffo-pancake-guard",
		PaymentMethod:   commerceschema.PaymentProviderStripe,
		PaymentProvider: commerceschema.PaymentProviderStripe,
		Status:          constant.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, db.Create(topUp).Error)

	err := RechargeWaffoPancake(topUp.TradeNo)
	require.Error(t, err)

	var savedTopUp commerceschema.TopUp
	require.NoError(t, db.Where("trade_no = ?", topUp.TradeNo).First(&savedTopUp).Error)
	assert.Equal(t, constant.TopUpStatusPending, savedTopUp.Status)

	var savedUser identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	assert.Equal(t, 0, savedUser.Quota)
}

func TestUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T) {
	tests := []struct {
		name                    string
		tradeNo                 string
		storedPaymentProvider   string
		expectedPaymentProvider string
		targetStatus            string
	}{
		{
			name:                    "stripe expire",
			tradeNo:                 "stripe-expire-guard",
			storedPaymentProvider:   commerceschema.PaymentProviderCreem,
			expectedPaymentProvider: commerceschema.PaymentProviderStripe,
			targetStatus:            constant.TopUpStatusExpired,
		},
		{
			name:                    "waffo failed",
			tradeNo:                 "waffo-failed-guard",
			storedPaymentProvider:   commerceschema.PaymentProviderStripe,
			expectedPaymentProvider: commerceschema.PaymentProviderWaffo,
			targetStatus:            constant.TopUpStatusFailed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			user := &identityschema.User{
				Id:       9000 + len(tc.tradeNo),
				Username: "payment_guard_user_" + tc.tradeNo,
				AffCode:  "aff_" + tc.tradeNo,
				Status:   constant.UserStatusEnabled,
			}
			require.NoError(t, db.Create(user).Error)

			topUp := &commerceschema.TopUp{
				UserId:          user.Id,
				Amount:          2,
				Money:           9.99,
				TradeNo:         tc.tradeNo,
				PaymentMethod:   tc.storedPaymentProvider,
				PaymentProvider: tc.storedPaymentProvider,
				Status:          constant.TopUpStatusPending,
				CreateTime:      time.Now().Unix(),
			}
			require.NoError(t, db.Create(topUp).Error)

			err := UpdatePendingTopUpStatus(tc.tradeNo, tc.expectedPaymentProvider, tc.targetStatus)
			require.ErrorIs(t, err, commerceschema.ErrPaymentMethodMismatch)

			var savedTopUp commerceschema.TopUp
			require.NoError(t, db.Where("trade_no = ?", tc.tradeNo).First(&savedTopUp).Error)
			assert.Equal(t, constant.TopUpStatusPending, savedTopUp.Status)
		})
	}
}

func TestExpireDueTopUps_ReleasesReservedBlindBoxProp(t *testing.T) {
	db := setupRedemptionTestDB(t)
	t.Setenv("TOPUP_PENDING_EXPIRY_MINUTES", "3")

	user := &identityschema.User{
		Id:       9010,
		Username: "stale_topup_user",
		Status:   constant.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	var prop *commerceschema.BlindBoxProp
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		prop, err = createBlindBoxPropTx(tx, user.Id, 2, "充值九折卡")
		return err
	}))

	topUp := &commerceschema.TopUp{
		UserId:     user.Id,
		Amount:     2,
		Money:      100,
		TradeNo:    "stale-topup-discount-order",
		Status:     constant.TopUpStatusPending,
		CreateTime: time.Now().Add(-4 * time.Minute).Unix(),
	}
	_, err := CreatePendingTopUpOrderWithBlindBoxDiscount(topUp)
	require.NoError(t, err)

	expired, err := ExpireDueTopUps(10)
	require.NoError(t, err)
	assert.Equal(t, 1, expired)

	var savedTopUp commerceschema.TopUp
	require.NoError(t, db.Where("trade_no = ?", topUp.TradeNo).First(&savedTopUp).Error)
	assert.Equal(t, constant.TopUpStatusExpired, savedTopUp.Status)

	var released commerceschema.BlindBoxProp
	require.NoError(t, db.Where("id = ?", prop.Id).First(&released).Error)
	assert.Equal(t, commerceschema.BlindBoxPropStatusAvailable, released.Status)
	assert.Zero(t, released.ReservedAt)
	assert.Empty(t, released.ReservedOrderType)
	assert.Empty(t, released.ReservedOrderTradeNo)
}
