package app

import (
	"testing"

	"github.com/sh2001sh/new-api/constant"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestGrantBlindBoxesIsIdempotentAndOpenable(t *testing.T) {
	db := setupRedemptionTestDB(t)
	require.NoError(t, db.AutoMigrate(&commerceschema.BlindBoxGrant{}))
	originalSetting := blindboxsettings.Get()
	setting := originalSetting
	setting.Enabled = true
	setting.DailyOpenLimit = 100
	setting.SubscriptionPrizeProbability = 0
	setting.PityThreshold = 999
	setting.FirstPurchaseGuaranteeUSD = 0
	setting.Tiers = []blindboxsettings.TierSetting{
		{Name: "admin grant reward", MinUSD: 1, MaxUSD: 1, Probability: 1},
	}
	blindboxsettings.Set(setting)
	t.Cleanup(func() { blindboxsettings.Set(originalSetting) })

	user := &identityschema.User{Id: 9921, Username: "admin-grant-user", Status: constant.UserStatusEnabled, AffCode: "AGU9921"}
	admin := &identityschema.User{Id: 9922, Username: "admin-grant-admin", Status: constant.UserStatusEnabled, AffCode: "AGA9922"}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(admin).Error)

	request := AdminBlindBoxGrantRequest{Quantity: 2, IdempotencyKey: "grant-key-1"}
	first, err := GrantBlindBoxes(user.Id, admin.Id, request)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.NotNil(t, first.Grant)
	require.NotNil(t, first.Order)
	require.Equal(t, commerceschema.BlindBoxOrderSourceAdminGrant, first.Order.Source)
	require.Equal(t, 2, first.Order.Quantity)

	second, err := GrantBlindBoxes(user.Id, admin.Id, request)
	require.NoError(t, err)
	require.Equal(t, first.Grant.Id, second.Grant.Id)

	var grantCount int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxGrant{}).Count(&grantCount).Error)
	require.Equal(t, int64(1), grantCount)

	records, err := OpenBlindBoxes(user.Id, 2)
	require.NoError(t, err)
	require.Len(t, records, 2)

	var order commerceschema.BlindBoxOrder
	require.NoError(t, db.First(&order, first.Order.Id).Error)
	require.Equal(t, 2, order.OpenedCount)
}
