package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExpireDueSubscriptionOrdersReleasesDiscountReservation(t *testing.T) {
	t.Setenv("SUBSCRIPTION_ORDER_PENDING_EXPIRY_MINUTES", "3")
	db := setupRedemptionTestDB(t)
	user := identityschema.User{Id: 9151, Username: "stale-subscription-order", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	var prop *commerceschema.BlindBoxProp
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		prop, err = createBlindBoxPropTx(tx, user.Id, 1, "套餐九折卡")
		return err
	}))
	order := commerceschema.SubscriptionOrder{
		UserId: user.Id, PlanId: 1, Money: 10, TradeNo: "stale-subscription-order",
		Status: constant.TopUpStatusPending, CreateTime: time.Now().Add(-4 * time.Minute).Unix(),
	}
	_, err := CreatePendingSubscriptionOrderWithDiscounts(&order, order.Money)
	require.NoError(t, err)

	expired, err := ExpireDueSubscriptionOrders(10)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	var saved commerceschema.SubscriptionOrder
	require.NoError(t, db.First(&saved, order.Id).Error)
	require.Equal(t, constant.TopUpStatusExpired, saved.Status)
	var released commerceschema.BlindBoxProp
	require.NoError(t, db.First(&released, prop.Id).Error)
	require.Equal(t, commerceschema.BlindBoxPropStatusAvailable, released.Status)
}

func TestExpireDueSubscriptionOrdersLeavesFreshOrderPending(t *testing.T) {
	t.Setenv("SUBSCRIPTION_ORDER_PENDING_EXPIRY_MINUTES", "3")
	db := setupRedemptionTestDB(t)
	order := commerceschema.SubscriptionOrder{
		UserId: 9152, PlanId: 1, Money: 10, TradeNo: "fresh-subscription-order",
		Status: constant.TopUpStatusPending, CreateTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&order).Error)
	expired, err := ExpireDueSubscriptionOrders(10)
	require.NoError(t, err)
	require.Zero(t, expired)
	var saved commerceschema.SubscriptionOrder
	require.NoError(t, db.First(&saved, order.Id).Error)
	require.Equal(t, constant.TopUpStatusPending, saved.Status)
}
