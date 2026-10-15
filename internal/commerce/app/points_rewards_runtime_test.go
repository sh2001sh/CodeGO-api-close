package app

import (
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"testing"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCommercePackagePurchasePoints_CreatesPointLedger(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{Id: 9301, Username: "package-points-user"}
	require.NoError(t, db.Create(user).Error)

	plan := &commerceschema.SubscriptionPlan{
		Id:    9302,
		Title: "Lite月卡",
	}
	require.NoError(t, db.Create(plan).Error)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return awardPackagePurchasePointsTx(tx, user.Id, plan, 9303)
	}))

	account, err := billingapp.EnsurePointAccountTx(db, user.Id)
	require.NoError(t, err)
	assert.Greater(t, account.Balance, int64(0))

	var ledgers []billingschema.PointLedger
	require.NoError(t, db.Where("user_id = ?", user.Id).Order("id asc").Find(&ledgers).Error)
	require.Len(t, ledgers, 1)
	assert.Equal(t, billingschema.PointLedgerTypeEarn, ledgers[0].Type)
	assert.Equal(t, billingschema.PointSourcePackagePurchase, ledgers[0].SourceType)
	assert.Equal(t, "9303", ledgers[0].SourceId)
	assert.Equal(t, account.Balance, ledgers[0].BalanceAfter)
}
