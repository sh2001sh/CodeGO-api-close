package app

import (
	"testing"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAddSubscriptionBonusTx_AllowsSameAmountFromDifferentCycles(t *testing.T) {
	db := setupRedemptionTestDB(t)
	subscription := &commerceschema.UserSubscription{Id: 99601, UserId: 99602, PlanId: 99603, AmountTotal: 1000, PeriodAmount: 1000}
	require.NoError(t, db.Create(subscription).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
			AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(subscription.Id), QuotaUnit: "quota",
		})
		if err != nil {
			return err
		}
		_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
			AccountID: account.AccountID, Amount: subscription.AmountTotal, IdempotencyKey: "bonus-cycle-bootstrap",
		})
		return err
	}))

	grant := func(key string) error {
		return db.Transaction(func(tx *gorm.DB) error {
			var locked commerceschema.UserSubscription
			if err := tx.First(&locked, subscription.Id).Error; err != nil {
				return err
			}
			return addSubscriptionBonusTx(tx, &locked, 500, key)
		})
	}
	require.NoError(t, grant("group-buy:51:member:13:tier:500"))
	require.NoError(t, grant("group-buy:59:member:49:tier:500"))
	require.NoError(t, grant("group-buy:59:member:49:tier:500"))

	var reloaded commerceschema.UserSubscription
	require.NoError(t, db.First(&reloaded, subscription.Id).Error)
	assert.EqualValues(t, 2000, reloaded.AmountTotal)
	assert.EqualValues(t, 2000, reloaded.PeriodAmount)

	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_type = ? AND owner_id = ?", "user_subscription", subscription.Id).First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
	assert.EqualValues(t, 2000, snapshot.AvailableBalance)

	var bonusEntries int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).
		Where("account_id = ? AND reason_code = ?", account.AccountID, "subscription_bonus").Count(&bonusEntries).Error)
	assert.EqualValues(t, 2, bonusEntries)
}
