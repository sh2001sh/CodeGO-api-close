package store

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReferralRebateMigrationPreservesExistingFundsAndRollout(t *testing.T) {
	oldPG := platformdb.UsingPostgreSQL
	platformdb.UsingPostgreSQL = false
	t.Cleanup(func() { platformdb.UsingPostgreSQL = oldPG })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	lotTable := (billingschema.FundingLot{}).TableName()
	require.NoError(t, db.Exec("CREATE TABLE "+lotTable+" (lot_id text primary key, original_amount bigint, remaining_amount bigint)").Error)
	allocationTable := (billingschema.FundingAllocation{}).TableName()
	require.NoError(t, db.Exec("CREATE TABLE "+allocationTable+" (allocation_id text primary key, amount bigint)").Error)
	require.NoError(t, db.Table(allocationTable).Create(map[string]any{"allocation_id": "historical", "amount": 3766}).Error)
	require.NoError(t, db.Exec("CREATE TABLE top_ups (id integer primary key, amount bigint, money numeric)").Error)
	require.NoError(t, db.Table(lotTable).Create(map[string]any{"lot_id": "historical", "original_amount": 5000, "remaining_amount": 1234}).Error)
	require.NoError(t, db.Table("top_ups").Create(map[string]any{"id": 9, "amount": 5000, "money": 10}).Error)
	before := time.Now().Unix()
	require.NoError(t, db.Transaction(migrateReferralConsumptionRebate))
	var program billingschema.ReferralRebateProgram
	require.NoError(t, db.First(&program, 1).Error)
	require.EqualValues(t, 300, program.RateBPS)
	require.GreaterOrEqual(t, program.EffectiveAt, before)
	require.LessOrEqual(t, program.EffectiveAt, time.Now().Unix())
	// Use a distinct timestamp to prove retries cannot move the rollout boundary.
	require.NoError(t, db.Model(&program).Update("effective_at", 123456).Error)
	require.NoError(t, db.Transaction(migrateReferralConsumptionRebate))
	require.NoError(t, db.First(&program, 1).Error)
	require.EqualValues(t, 123456, program.EffectiveAt)
	var lot billingschema.FundingLot
	require.NoError(t, db.First(&lot, "lot_id = ?", "historical").Error)
	require.EqualValues(t, 5000, lot.OriginalAmount)
	require.EqualValues(t, 1234, lot.RemainingAmount)
	require.Zero(t, lot.PaidQuota, "unknown historical cash cannot become rebate-eligible")
	var allocation billingschema.FundingAllocation
	require.NoError(t, db.First(&allocation, "allocation_id = ?", "historical").Error)
	require.EqualValues(t, 3766, allocation.Amount)
	require.Zero(t, allocation.PaidQuota)
	var topup commerceschema.TopUp
	require.NoError(t, db.First(&topup, 9).Error)
	require.EqualValues(t, 5000, topup.Amount)
	require.EqualValues(t, 10, topup.Money)
	require.Zero(t, topup.PaidQuota)
	var receipts int64
	require.NoError(t, db.Model(&billingschema.ReferralConsumptionRebate{}).Count(&receipts).Error)
	require.Zero(t, receipts, "migration must not retroactively reward historical consumption")
	require.Contains(t, V2MigrationIDs(), "20261015_referral_consumption_rebate")
}
