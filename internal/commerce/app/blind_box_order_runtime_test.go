package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestCompleteBlindBoxOrderKeepsBoxesForUserSelectedReveal(t *testing.T) {
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
	require.Zero(t, saved.OpenedCount)
	var opens int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxOpenRecord{}).Where("order_id = ?", order.Id).Count(&opens).Error)
	require.Zero(t, opens)
}
