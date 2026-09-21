package app

import (
	"math"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceReclaimTransfersAmountAboveInt32(t *testing.T) {
	setupMonthlyPassFundingTestDB(t)
	db := platformdb.DB
	reclaimAmount := int64(math.MaxInt32) + 313_199_320
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}, &marketplaceschema.IncomeReclaim{}, &billingschema.FundingLot{}))
	require.NoError(t, db.Create([]identityschema.User{
		{Id: 1, Username: "bigint-admin", AffCode: "bigint-admin"},
		{Id: 10, Username: "bigint-owner", AffCode: "bigint-owner", ClaudeQuota: int(reclaimAmount)},
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		ID: "bigint-reclaim", RequestID: "bigint-reclaim", GroupID: "g-bigint",
		OwnerUserID: 10, OwnerNetAmount: reclaimAmount, Status: "released",
	}).Error)
	marketplacesettlement.RegisterReclaimHook(ReclaimMarketplaceOwnerEarningsTx)
	t.Cleanup(func() { marketplacesettlement.RegisterReclaimHook(nil) })

	result, err := marketplacesettlement.ReclaimPending(marketplacesettlement.ReleaseFilter{
		OwnerUserIDs: []int{10}, MaxAmount: reclaimAmount, OperationID: "bigint-wallet-reclaim",
	})
	require.NoError(t, err)
	require.Equal(t, reclaimAmount, result.Amount)

	var owner, admin identityschema.User
	require.NoError(t, db.First(&owner, 10).Error)
	require.NoError(t, db.First(&admin, 1).Error)
	require.Zero(t, owner.ClaudeQuota)
	require.EqualValues(t, reclaimAmount, admin.ClaudeQuota)
	ownerBalance, err := GetUserClaudeWalletQuota(10)
	require.NoError(t, err)
	require.Zero(t, ownerBalance)
	adminBalance, err := GetUserClaudeWalletQuota(1)
	require.NoError(t, err)
	require.EqualValues(t, reclaimAmount, adminBalance)
}

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
	var operations int64
	require.NoError(t, db.Model(&marketplaceschema.IncomeReclaim{}).Where("id = ?", "insufficient-wallet").Count(&operations).Error)
	require.EqualValues(t, 1, operations)
}
