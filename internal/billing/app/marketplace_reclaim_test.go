package app

import (
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMarketplaceReclaimUpdatesWalletsAndRollsBackWholeBatch(t *testing.T) {
	setupMonthlyPassFundingTestDB(t)
	db := platformdb.DB
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}, &marketplaceschema.IncomeReclaim{}, &billingschema.FundingLot{}))
	require.NoError(t, db.Create([]identityschema.User{
		{Id: 1, Username: "admin", AffCode: "admin", ClaudeQuota: 100},
		{Id: 10, Username: "owner", AffCode: "owner", ClaudeQuota: 95},
		{Id: 11, Username: "spent-owner", AffCode: "spent-owner", ClaudeQuota: 0},
	}).Error)
	reference := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, db.Create([]marketplaceschema.Settlement{
		{ID: "first", RequestID: "first", GroupID: "g1", OwnerUserID: 10, OwnerNetAmount: 95, Status: "released", CreatedAt: reference.Add(-time.Minute)},
		{ID: "second", RequestID: "second", GroupID: "g2", OwnerUserID: 11, OwnerNetAmount: 50, Status: "released", CreatedAt: reference},
	}).Error)
	marketplacesettlement.RegisterReclaimHook(ReclaimMarketplaceOwnerEarningsTx)
	t.Cleanup(func() { marketplacesettlement.RegisterReclaimHook(nil) })
	filter := marketplacesettlement.ReleaseFilter{OwnerUserIDs: []int{10}, MaxAmount: 40, OperationID: "exact-wallet-reclaim"}
	result, err := marketplacesettlement.ReclaimPending(filter)
	require.NoError(t, err)
	require.EqualValues(t, 40, result.Amount)
	require.Equal(t, map[int]int64{10: 40}, result.OwnerAmounts)
	repeated, err := marketplacesettlement.ReclaimPending(filter)
	require.NoError(t, err)
	require.Equal(t, result, repeated)
	var owner, admin identityschema.User
	require.NoError(t, db.First(&owner, 10).Error)
	require.NoError(t, db.First(&admin, 1).Error)
	require.Equal(t, 55, owner.ClaudeQuota)
	require.Equal(t, 140, admin.ClaudeQuota)
	ownerBalance, err := GetUserClaudeWalletQuota(10)
	require.NoError(t, err)
	require.Equal(t, 55, ownerBalance)
	adminBalance, err := GetUserClaudeWalletQuota(1)
	require.NoError(t, err)
	require.Equal(t, 140, adminBalance)
	var ledgerBefore, ledgerAfter int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&ledgerBefore).Error)
	failed, err := marketplacesettlement.ReclaimPending(marketplacesettlement.ReleaseFilter{
		OwnerUserIDs: []int{10, 11}, OperationID: "insufficient-wallet",
	})
	require.Error(t, err)
	require.Zero(t, failed)
	require.NoError(t, db.First(&owner, 10).Error)
	require.NoError(t, db.First(&admin, 1).Error)
	require.Equal(t, 55, owner.ClaudeQuota)
	require.Equal(t, 140, admin.ClaudeQuota)
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&ledgerAfter).Error)
	require.Equal(t, ledgerBefore, ledgerAfter)
	var record marketplaceschema.Settlement
	require.NoError(t, db.First(&record, "id = ?", "first").Error)
	require.EqualValues(t, 40, record.ReclaimedAmount)
	require.Equal(t, "released", record.Status)
	var failedOperation marketplaceschema.IncomeReclaim
	require.NoError(t, db.First(&failedOperation, "id = ?", "insufficient-wallet").Error)
	require.Equal(t, "failed", failedOperation.Status)
	require.NotEmpty(t, failedOperation.ErrorMessage)
	require.Zero(t, failedOperation.Count)
	require.Zero(t, failedOperation.Amount)
}

func TestDirectMarketplaceReclaimUsesSpendableWalletAndPreservesReservations(t *testing.T) {
	setupMonthlyPassFundingTestDB(t)
	db := platformdb.DB
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}, &marketplaceschema.IncomeReclaim{}, &billingschema.FundingLot{}))
	require.NoError(t, db.Create([]identityschema.User{{Id: 1, Username: "direct-admin", AffCode: "direct-admin", ClaudeQuota: 100}, {Id: 10, Username: "direct-owner", AffCode: "direct-owner", ClaudeQuota: 95}}).Error)
	var accountID string
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		a, err := ensureMirroredUserAccountTx(tx, 10, billingAccountTypeClaudeWallet, 95)
		if err != nil {
			return err
		}
		accountID = a.AccountID
		return tx.Model(&billingschema.BillingBalanceSnapshot{}).Where("account_id = ?", accountID).Updates(map[string]any{"available_balance": 35, "reserved_balance": 60}).Error
	}))
	require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", 10).Update("claude_quota", 35).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{ID: "direct-money", RequestID: "direct-money", GroupID: "g", OwnerUserID: 10, OwnerNetAmount: 95, Status: "released"}).Error)
	marketplacesettlement.RegisterReclaimHook(ReclaimMarketplaceOwnerEarningsTx)
	marketplacesettlement.RegisterReclaimBalancesHook(LockMarketplaceReclaimBalancesTx)
	t.Cleanup(func() {
		marketplacesettlement.RegisterReclaimHook(nil)
		marketplacesettlement.RegisterReclaimBalancesHook(nil)
	})
	overLimit, err := marketplacesettlement.CreateIncomeReclaimTask(marketplacesettlement.ReleaseFilter{
		Direct: true, OwnerUserIDs: []int{10}, MaxAmount: 36, OperationID: "direct-over-wallet-limit",
	})
	require.NoError(t, err)
	_, err = marketplacesettlement.ProcessDirectIncomeReclaimTask(overLimit.ID)
	require.ErrorContains(t, err, "可用余额不足")
	var unchangedOwner, unchangedAdmin identityschema.User
	require.NoError(t, db.First(&unchangedOwner, 10).Error)
	require.NoError(t, db.First(&unchangedAdmin, 1).Error)
	require.Equal(t, 35, unchangedOwner.ClaudeQuota)
	require.Equal(t, 100, unchangedAdmin.ClaudeQuota)
	var unchangedBalance billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&unchangedBalance, "account_id = ?", accountID).Error)
	require.EqualValues(t, 35, unchangedBalance.AvailableBalance)
	require.EqualValues(t, 60, unchangedBalance.ReservedBalance)

	filter := marketplacesettlement.ReleaseFilter{Direct: true, OwnerUserIDs: []int{10}, OperationID: "direct-money"}
	task, err := marketplacesettlement.CreateIncomeReclaimTask(filter)
	require.NoError(t, err)
	task, err = marketplacesettlement.ProcessDirectIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.EqualValues(t, 35, task.Amount)
	var balance billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&balance, "account_id = ?", accountID).Error)
	require.Zero(t, balance.AvailableBalance)
	require.EqualValues(t, 60, balance.ReservedBalance)
	var admin identityschema.User
	require.NoError(t, db.First(&admin, 1).Error)
	require.Equal(t, 135, admin.ClaudeQuota)
	var history marketplaceschema.Settlement
	require.NoError(t, db.First(&history, "id = ?", "direct-money").Error)
	require.Zero(t, history.ReclaimedAmount)
	_, err = marketplacesettlement.CreateIncomeReclaimTask(filter)
	require.NoError(t, err)
	_, err = marketplacesettlement.ProcessDirectIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	_, err = marketplacesettlement.ProcessDirectIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.NoError(t, db.First(&admin, 1).Error)
	require.Equal(t, 135, admin.ClaudeQuota)
	require.NoError(t, db.First(&history, "id = ?", "direct-money").Error)
	require.EqualValues(t, 35, history.ReclaimedAmount)
}

func TestDirectReclaimReceiptFailureRollsBackRealWalletTransfers(t *testing.T) {
	setupMonthlyPassFundingTestDB(t)
	db := platformdb.DB
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}, &marketplaceschema.IncomeReclaim{}, &billingschema.FundingLot{}))
	require.NoError(t, db.Create([]identityschema.User{{Id: 1, Username: "rollback-admin", AffCode: "rollback-admin", ClaudeQuota: 100}, {Id: 10, Username: "rollback-owner", AffCode: "rollback-owner", ClaudeQuota: 95}}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{ID: "rollback-money", RequestID: "rollback-money", GroupID: "g", OwnerUserID: 10, OwnerNetAmount: 95, Status: "released"}).Error)
	marketplacesettlement.RegisterReclaimHook(ReclaimMarketplaceOwnerEarningsTx)
	marketplacesettlement.RegisterReclaimBalancesHook(LockMarketplaceReclaimBalancesTx)
	t.Cleanup(func() {
		marketplacesettlement.RegisterReclaimHook(nil)
		marketplacesettlement.RegisterReclaimBalancesHook(nil)
	})
	task, err := marketplacesettlement.CreateIncomeReclaimTask(marketplacesettlement.ReleaseFilter{Direct: true, OwnerUserIDs: []int{10}, OperationID: "rollback-money"})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_charged_receipt BEFORE UPDATE ON marketplace_income_reclaims WHEN NEW.status = 'charged' BEGIN SELECT RAISE(ABORT, 'forced receipt failure'); END").Error)
	_, err = marketplacesettlement.ProcessDirectIncomeReclaimTask(task.ID)
	require.ErrorContains(t, err, "forced receipt failure")
	var users []identityschema.User
	require.NoError(t, db.Where("id IN ?", []int{1, 10}).Order("id").Find(&users).Error)
	require.Equal(t, 100, users[0].ClaudeQuota)
	require.Equal(t, 95, users[1].ClaudeQuota)
	var entries int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&entries).Error)
	require.Zero(t, entries)
	task, err = marketplacesettlement.GetIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", task.Status)
	require.Zero(t, task.Amount)
}
