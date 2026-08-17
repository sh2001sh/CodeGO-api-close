package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestCompleteBlindBoxOrderCreatesSealedUnifiedInventory(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8891, Username: "blind-box-manual-reveal", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	order := &commerceschema.BlindBoxOrder{
		UserId: user.Id, Quantity: 3, TradeNo: "blind-box-manual-reveal-order",
		PaymentMethod: "test", PaymentProvider: "test", Status: constant.TopUpStatusPending,
		CreateTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	require.NoError(t, CompleteBlindBoxOrder(order.TradeNo, "paid", "test", "test"))

	var saved commerceschema.BlindBoxOrder
	require.NoError(t, db.First(&saved, order.Id).Error)
	require.Equal(t, constant.TopUpStatusSuccess, saved.Status)
	require.Equal(t, saved.Quantity, saved.OpenedCount)
	var opens int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxOpenRecord{}).Where("order_id = ?", order.Id).Count(&opens).Error)
	require.Zero(t, opens)
	var inventory int64
	require.NoError(t, db.Model(&commerceschema.BalanceBlindBoxItem{}).
		Where("owner_user_id = ? AND status = ?", user.Id, commerceschema.BalanceBlindBoxItemStatusAvailable).
		Count(&inventory).Error)
	require.Equal(t, int64(3), inventory)
}

func TestCancelPendingBlindBoxOrderReleasesPurchaseLimit(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setPaidBlindBoxTestSetting(t, 2)
	user := &identityschema.User{Id: 8892, Username: "blind-box-cancel", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	order := &commerceschema.BlindBoxOrder{
		UserId: user.Id, Quantity: 2, Money: 5, TradeNo: "blind-box-cancel-order",
		PaymentMethod: "test", PaymentProvider: "test", Status: constant.TopUpStatusPending,
		CreateTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	_, err := ValidateBlindBoxPurchase(user.Id, 1)
	require.ErrorContains(t, err, "daily blind box limit reached")
	require.NoError(t, CancelPendingBlindBoxOrder(user.Id, order.TradeNo))
	require.NoError(t, CancelPendingBlindBoxOrder(user.Id, order.TradeNo))

	amount, err := ValidateBlindBoxPurchase(user.Id, 1)
	require.NoError(t, err)
	require.Equal(t, 2.5, amount)
	var saved commerceschema.BlindBoxOrder
	require.NoError(t, db.First(&saved, order.Id).Error)
	require.Equal(t, constant.TopUpStatusExpired, saved.Status)
}

func TestCancelPendingBlindBoxOrderRejectsAnotherUser(t *testing.T) {
	db := setupRedemptionTestDB(t)
	order := &commerceschema.BlindBoxOrder{
		UserId: 8893, Quantity: 1, Money: 2.5, TradeNo: "blind-box-owned-order",
		PaymentMethod: "test", PaymentProvider: "test", Status: constant.TopUpStatusPending,
		CreateTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	err := CancelPendingBlindBoxOrder(8894, order.TradeNo)
	require.ErrorIs(t, err, commercedomain.ErrBlindBoxOrderNotFound)
	var saved commerceschema.BlindBoxOrder
	require.NoError(t, db.First(&saved, order.Id).Error)
	require.Equal(t, constant.TopUpStatusPending, saved.Status)
}

func TestCompleteBlindBoxOrderRestoresExpiredOrderAfterVerifiedPayment(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8895, Username: "blind-box-late-payment", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	order := &commerceschema.BlindBoxOrder{
		UserId: user.Id, Quantity: 2, Money: 5, TradeNo: "blind-box-late-payment-order",
		PaymentMethod: "test", PaymentProvider: "test", Status: constant.TopUpStatusExpired,
		CreateTime: time.Now().Unix(), CompleteTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	require.NoError(t, CompleteBlindBoxOrder(order.TradeNo, "paid-late", "test", "test"))
	require.NoError(t, CompleteBlindBoxOrder(order.TradeNo, "paid-late", "test", "test"))

	var saved commerceschema.BlindBoxOrder
	require.NoError(t, db.First(&saved, order.Id).Error)
	require.Equal(t, constant.TopUpStatusSuccess, saved.Status)
	var inventory int64
	require.NoError(t, db.Model(&commerceschema.BalanceBlindBoxItem{}).
		Where("owner_user_id = ?", user.Id).Count(&inventory).Error)
	require.Equal(t, int64(2), inventory)
}

func TestExpireDueBlindBoxOrdersReleasesStalePurchaseLimit(t *testing.T) {
	t.Setenv("BLIND_BOX_PENDING_EXPIRY_MINUTES", "1")
	db := setupRedemptionTestDB(t)
	setPaidBlindBoxTestSetting(t, 1)
	user := &identityschema.User{Id: 8896, Username: "blind-box-stale", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	order := &commerceschema.BlindBoxOrder{
		UserId: user.Id, Quantity: 1, Money: 2.5, TradeNo: "blind-box-stale-order",
		PaymentMethod: "test", PaymentProvider: "test", Status: constant.TopUpStatusPending,
		CreateTime: time.Now().Add(-2 * time.Minute).Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	amount, err := ValidateBlindBoxPurchase(user.Id, 1)
	require.NoError(t, err)
	require.Equal(t, 2.5, amount)
	expired, err := ExpireDueBlindBoxOrders(10)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	var saved commerceschema.BlindBoxOrder
	require.NoError(t, db.First(&saved, order.Id).Error)
	require.Equal(t, constant.TopUpStatusExpired, saved.Status)
}

func setPaidBlindBoxTestSetting(t *testing.T, limit int) {
	t.Helper()
	original := blindboxsettings.Get()
	setting := original
	setting.Enabled = true
	setting.UnitPrice = 2.5
	setting.DailyLimit = limit
	setting.MonthlyLimit = limit
	blindboxsettings.Set(setting)
	t.Cleanup(func() { blindboxsettings.Set(original) })
}
