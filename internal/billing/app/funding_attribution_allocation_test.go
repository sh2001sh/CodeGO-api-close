package app

import (
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestAllocateSettledFundingFIFOConsumesOnlyRequiredLots(t *testing.T) {
	require.NoError(t, platformdb.DB.AutoMigrate(&billingschema.FundingLot{}, &billingschema.FundingAllocation{}))
	require.NoError(t, platformdb.DB.Exec("DELETE FROM billing_funding_allocations").Error)
	require.NoError(t, platformdb.DB.Exec("DELETE FROM billing_funding_lots").Error)
	t.Cleanup(func() {
		platformdb.DB.Exec("DELETE FROM billing_funding_allocations")
		platformdb.DB.Exec("DELETE FROM billing_funding_lots")
	})

	const accountID = "funding-fifo-account"
	lots := []billingschema.FundingLot{
		{LotID: "lot-old", AccountID: accountID, Source: billingschema.FundingSourceTopup, OriginalAmount: 600, RemainingAmount: 600, IdempotencyKey: "lot-old", CreatedAt: time.Unix(1, 0)},
		{LotID: "lot-next", AccountID: accountID, Source: billingschema.FundingSourceBlindBox, OriginalAmount: 900, RemainingAmount: 900, IdempotencyKey: "lot-next", CreatedAt: time.Unix(2, 0)},
		{LotID: "lot-unused", AccountID: accountID, Source: billingschema.FundingSourceOther, OriginalAmount: 1_200, RemainingAmount: 1_200, IdempotencyKey: "lot-unused", CreatedAt: time.Unix(3, 0)},
	}
	require.NoError(t, platformdb.DB.Create(&lots).Error)

	require.NoError(t, AllocateSettledFundingFIFO("request-fifo", accountID, 750))

	var stored []billingschema.FundingLot
	require.NoError(t, platformdb.DB.Where("account_id = ?", accountID).Order("created_at asc").Find(&stored).Error)
	require.EqualValues(t, 0, stored[0].RemainingAmount)
	require.EqualValues(t, 750, stored[1].RemainingAmount)
	require.EqualValues(t, 1_200, stored[2].RemainingAmount)
	var allocations []billingschema.FundingAllocation
	require.NoError(t, platformdb.DB.Where("request_id = ?", "request-fifo").Order("lot_id asc").Find(&allocations).Error)
	require.Len(t, allocations, 2)
	require.EqualValues(t, 750, allocations[0].Amount+allocations[1].Amount)

	require.NoError(t, AllocateSettledFundingFIFO("request-fifo", accountID, 750))
	var allocationCount int64
	require.NoError(t, platformdb.DB.Model(&billingschema.FundingAllocation{}).Where("request_id = ?", "request-fifo").Count(&allocationCount).Error)
	require.EqualValues(t, 2, allocationCount)
}

func TestAllocateSettledFundingFIFOCreatesOnlyMissingLegacyAmount(t *testing.T) {
	require.NoError(t, platformdb.DB.AutoMigrate(&billingschema.FundingLot{}, &billingschema.FundingAllocation{}))
	require.NoError(t, platformdb.DB.Exec("DELETE FROM billing_funding_allocations").Error)
	require.NoError(t, platformdb.DB.Exec("DELETE FROM billing_funding_lots").Error)
	t.Cleanup(func() {
		platformdb.DB.Exec("DELETE FROM billing_funding_allocations")
		platformdb.DB.Exec("DELETE FROM billing_funding_lots")
	})

	const accountID = "funding-legacy-gap"
	require.NoError(t, platformdb.DB.Create(&billingschema.FundingLot{
		LotID: "known-lot", AccountID: accountID, Source: billingschema.FundingSourceTopup,
		OriginalAmount: 200, RemainingAmount: 200, IdempotencyKey: "known-lot",
	}).Error)

	require.NoError(t, AllocateSettledFundingFIFO("request-legacy-gap", accountID, 500))

	var legacy billingschema.FundingLot
	require.NoError(t, platformdb.DB.Where("account_id = ? AND source = ?", accountID, billingschema.FundingSourceLegacyUnattributed).First(&legacy).Error)
	require.EqualValues(t, 300, legacy.OriginalAmount)
	require.Zero(t, legacy.RemainingAmount)
	var allocated int64
	require.NoError(t, platformdb.DB.Model(&billingschema.FundingAllocation{}).Where("request_id = ?", "request-legacy-gap").Select("COALESCE(SUM(amount), 0)").Scan(&allocated).Error)
	require.EqualValues(t, 500, allocated)
}
