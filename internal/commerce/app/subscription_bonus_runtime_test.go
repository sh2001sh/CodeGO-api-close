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

func TestAddSubscriptionBonusTx_MaterializesOnlyUnbackedBalance(t *testing.T) {
	for _, tc := range []struct {
		name          string
		existing      bool
		ledgerAmount  int64
		ledgerMarker  bool
		used          int64
		wantAvailable int64
		wantBaseline  int64
	}{
		{name: "new account", used: 600, wantAvailable: 600, wantBaseline: 400},
		{name: "empty account", existing: true, used: 600, wantAvailable: 600, wantBaseline: 400},
		{name: "authoritative ledger", existing: true, ledgerAmount: 75, used: 600, wantAvailable: 275},
		{name: "zero balance authoritative ledger", existing: true, ledgerMarker: true, used: 600, wantAvailable: 200},
		{name: "exhausted projection", used: 1000, wantAvailable: 200},
		{name: "overspent projection", used: 1100, wantAvailable: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			sub := &commerceschema.UserSubscription{Id: 99701, UserId: 99702, PlanId: 99703, AmountTotal: 1000, AmountUsed: tc.used, PeriodAmount: 1000}
			require.NoError(t, db.Create(sub).Error)
			if tc.existing {
				account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota"})
				require.NoError(t, err)
				if tc.ledgerAmount > 0 {
					_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: tc.ledgerAmount, IdempotencyKey: "bonus-authoritative"})
					require.NoError(t, err)
				}
				if tc.ledgerMarker {
					_, err = billingdomain.RecordAdjustment(billingdomain.RecordAdjustmentParams{AccountID: account.AccountID, IdempotencyKey: "bonus-authoritative-zero"})
					require.NoError(t, err)
				}
			}
			grant := func(amount int64) error {
				// A caller retaining an old projection must not overwrite current totals.
				stale := *sub
				stale.AmountTotal = 1
				return db.Transaction(func(tx *gorm.DB) error { return addSubscriptionBonusTx(tx, &stale, amount, "materialize") })
			}
			require.NoError(t, grant(200))
			require.NoError(t, grant(200))

			var reloaded commerceschema.UserSubscription
			require.NoError(t, db.First(&reloaded, sub.Id).Error)
			assert.EqualValues(t, 1200, reloaded.AmountTotal)
			assert.EqualValues(t, 1200, reloaded.PeriodAmount)
			assert.Equal(t, tc.used, reloaded.AmountUsed)
			var account billingschema.BillingAccount
			require.NoError(t, db.Where("owner_type = ? AND owner_id = ?", "user_subscription", sub.Id).First(&account).Error)
			var snapshot billingschema.BillingBalanceSnapshot
			require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
			assert.Equal(t, tc.wantAvailable, snapshot.AvailableBalance)
			var baseline int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ? AND reason_code = ?", account.AccountID, "subscription_balance_bootstrap").Select("COALESCE(SUM(amount), 0)").Scan(&baseline).Error)
			assert.Equal(t, tc.wantBaseline, baseline)
			var bonuses int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ? AND reason_code = ?", account.AccountID, "subscription_bonus").Count(&bonuses).Error)
			assert.EqualValues(t, 1, bonuses)
			require.ErrorIs(t, grant(201), billingdomain.ErrLedgerConflict)
			require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
			assert.Equal(t, tc.wantAvailable, snapshot.AvailableBalance)
		})
	}
}

func TestAddSubscriptionBonusTx_RollsBackMaterializationWithProjection(t *testing.T) {
	db := setupRedemptionTestDB(t)
	sub := &commerceschema.UserSubscription{Id: 99801, UserId: 99802, PlanId: 99803, AmountTotal: 1000, AmountUsed: 600, PeriodAmount: 1000}
	require.NoError(t, db.Create(sub).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota"})
	require.NoError(t, err)
	grant := func() error {
		return db.Transaction(func(tx *gorm.DB) error { return addSubscriptionBonusTx(tx, sub, 200, "rollback") })
	}
	require.NoError(t, db.Exec("CREATE TRIGGER reject_bonus_projection BEFORE UPDATE ON user_subscriptions BEGIN SELECT RAISE(ABORT, 'reject bonus projection'); END").Error)
	require.ErrorContains(t, grant(), "reject bonus projection")
	var entries, outbox int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&entries).Error)
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Count(&outbox).Error)
	assert.Zero(t, entries)
	assert.Zero(t, outbox)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
	assert.Zero(t, snapshot.AvailableBalance)
	var reloaded commerceschema.UserSubscription
	require.NoError(t, db.First(&reloaded, sub.Id).Error)
	assert.EqualValues(t, 1000, reloaded.AmountTotal)
	assert.EqualValues(t, 1000, sub.AmountTotal)
	require.NoError(t, db.Exec("DROP TRIGGER reject_bonus_projection").Error)
	require.NoError(t, grant())
	require.NoError(t, grant())
	require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
	assert.EqualValues(t, 600, snapshot.AvailableBalance)
}
