package settlement

import (
	"encoding/json"
	"fmt"
	"testing"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestPaidBonusFeePolicyUsesPurchaseBonusRate(t *testing.T) {
	for _, bonus := range []int64{0, 3, 5, 6, 7, 8, 10} {
		t.Run(fmt.Sprint(bonus), func(t *testing.T) {
			db := openSettlementTestDB(t)
			params := RecordParams{
				RequestID: "paid-bonus", GroupID: "group", OwnerUserID: 10, ConsumerUserID: 20,
				BillingSource: "subscription", ConsumerDebitAmount: 100000, SettlementGrossAmount: 100000,
				CreditPackGrantedQuota: 100000 + bonus*1000, CreditPackPaidQuota: 100000, CreditPackChannelShareBPS: 6000,
				FeePolicyVersion: FeePolicyPaidBonusV2, SubscriptionID: 44, SubscriptionPlanID: 9, SubscriptionPlanTitle: "Purchased pack",
			}
			require.NoError(t, Record(params))
			var item marketplaceschema.Settlement
			require.NoError(t, db.First(&item, "request_id = ?", params.RequestID).Error)
			require.EqualValues(t, 5000, item.PlatformCommission)
			require.EqualValues(t, bonus*600, item.TransactionFee)
			require.EqualValues(t, 95000-bonus*600, item.OwnerNetAmount)
			var entry billingschema.BillingLedgerEntry
			require.NoError(t, db.First(&entry, "idempotency_key = ?", "marketplace-pending:"+params.RequestID).Error)
			var snapshot IncomeSnapshot
			require.NoError(t, json.Unmarshal(entry.Metadata, &snapshot))
			require.Equal(t, FeePolicyPaidBonusV2, snapshot.AlgorithmVersion)
			require.Equal(t, 44, snapshot.SubscriptionID)
			require.Equal(t, "Purchased pack", snapshot.SubscriptionPlanTitle)
			require.InDelta(t, float64(bonus)/100*.6, snapshot.PackageSurchargeRate, 1e-12)
			originalMetadata := string(entry.Metadata)
			params.SubscriptionPlanTitle = "Changed plan"
			params.CreditPackGrantedQuota = 120000
			require.NoError(t, Record(params))
			require.NoError(t, db.First(&entry, "entry_id = ?", entry.EntryID).Error)
			require.Equal(t, originalMetadata, string(entry.Metadata))
			require.NoError(t, db.First(&item, "id = ?", item.ID).Error)
			require.EqualValues(t, bonus*600, item.TransactionFee)
		})
	}
}

func TestPaidBonusPolicyRejectsOverflowAndUnknownVersions(t *testing.T) {
	for _, params := range []RecordParams{
		{FeePolicyVersion: FeePolicyPaidBonusV2, CreditPackGrantedQuota: 9000000000000000000, CreditPackPaidQuota: 1, CreditPackChannelShareBPS: 6000},
		{FeePolicyVersion: "future-unknown"},
	} {
		db := openSettlementTestDB(t)
		params.RequestID, params.GroupID, params.OwnerUserID, params.SettlementGrossAmount = "invalid", "group", 10, 9000000000000000000
		require.Error(t, Record(params))
		var count int64
		require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestPaidBonusPlatformSubsidyCannotBeNegative(t *testing.T) {
	db := openSettlementTestDB(t)
	params := RecordParams{
		RequestID: "bounded-subsidy", GroupID: "group", OwnerUserID: 10, ConsumerUserID: 20,
		BillingSource: "subscription", ConsumerDebitAmount: 100000, SettlementGrossAmount: 100000,
		CreditPackGrantedQuota: 110000, CreditPackPaidQuota: 100000, CreditPackChannelShareBPS: 10000,
		FeePolicyVersion: FeePolicyPaidBonusV2,
	}
	require.NoError(t, Record(params))
	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", params.RequestID).Error)
	require.Greater(t, item.TransactionFee, item.CreditPackDiscountAmount)
	require.Zero(t, item.CreditPackPlatformSubsidyAmount)
	require.EqualValues(t, 10000, item.TransactionFee, "historical fee policy stays exact")
	require.EqualValues(t, 85000, item.OwnerNetAmount)
	require.Equal(t, params.SettlementGrossAmount, item.OwnerNetAmount+item.PlatformCommission+item.TransactionFee)
}

func TestRecordCreditPackSharesBonusCostAndKeepsPurchaseSnapshot(t *testing.T) {
	db := openSettlementTestDB(t)
	// 100 paid credits buy 110 credits. Of the 10 bonus credits, the
	// channel funds 6 and the platform funds 4, alongside the normal 5% fee.
	params := RecordParams{
		RequestID: "credit-pack", GroupID: "group", OwnerUserID: 10,
		ConsumerUserID: 20, BillingSource: "subscription", ConsumerDebitAmount: 110000,
		SettlementGrossAmount: 110000, WalletMultiplier: 1,
		CreditPackGrantedQuota: 110000, CreditPackPaidQuota: 100000, CreditPackChannelShareBPS: 6000,
	}
	require.NoError(t, Record(params))
	// A replay retains the original fee policy and cannot credit the ledger twice.
	params.CreditPackChannelShareBPS = 7000
	params.FeePolicyVersion = FeePolicyPaidBonusV2
	require.NoError(t, Record(params))

	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", params.RequestID).Error)
	require.EqualValues(t, 5500, item.PlatformCommission)
	require.EqualValues(t, 6000, item.TransactionFee)
	require.EqualValues(t, 98500, item.OwnerNetAmount)
	require.EqualValues(t, 10000, item.CreditPackDiscountAmount)
	require.EqualValues(t, 4000, item.CreditPackPlatformSubsidyAmount)
	require.EqualValues(t, 110000, item.CreditPackGrantedQuota)
	require.EqualValues(t, 100000, item.CreditPackPaidQuota)
	require.EqualValues(t, 6000, item.CreditPackChannelShareBPS)
	// Platform revenue is gross fees, while cash retained after funding the
	// bonus is 1.5 credits. Fees must not be presented as net profit.
	require.EqualValues(t, 1500, item.PlatformCommission+item.TransactionFee-item.CreditPackDiscountAmount)

	var ownerBalance billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&ownerBalance, "account_id = ?", item.PendingAccountID).Error)
	require.EqualValues(t, 98500, ownerBalance.AvailableBalance)
	var platformAccount billingschema.BillingAccount
	require.NoError(t, db.First(&platformAccount, "account_type = ?", "marketplace_platform_revenue").Error)
	var platformBalance billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&platformBalance, "account_id = ?", platformAccount.AccountID).Error)
	require.EqualValues(t, 11500, platformBalance.AvailableBalance)
	var count int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestPaidBonusFeeRoundingAndLargeIntermediates(t *testing.T) {
	for _, test := range []struct{ gross, granted, paid, share, discount, fee int64 }{
		{5, 120, 100, 6000, 1, 1},
		{6, 110, 100, 6000, 1, 0},
		{8800000000000000000, 8800000000000000000, 8000000000000000000, 6000, 800000000000000000, 528000000000000000},
		{100, 0, 0, 0, 0, 0},
	} {
		discount, fee, err := creditPackCharges(RecordParams{
			FeePolicyVersion: FeePolicyPaidBonusV2, BillingSource: "wallet", SettlementGrossAmount: test.gross,
			CreditPackGrantedQuota: test.granted, CreditPackPaidQuota: test.paid, CreditPackChannelShareBPS: test.share,
		})
		require.NoError(t, err)
		require.Equal(t, test.discount, discount)
		require.Equal(t, test.fee, fee)
	}
}

func TestRecordCreditPackPartialConsumptionAndUnchangedLegacyCharges(t *testing.T) {
	tests := []struct {
		name                                    string
		source                                  string
		gross, debit, granted, paid, share      int64
		commission, fee, discount, subsidy, net int64
	}{
		{name: "partial", source: "subscription", gross: 22000, debit: 22000, granted: 110000, paid: 100000, share: 6000, commission: 1100, fee: 1200, discount: 2000, subsidy: 800, net: 19700},
		{name: "wallet", source: "wallet", gross: 100000, debit: 100000, commission: 5000, net: 95000},
		{name: "legacy tenfold debit", source: "subscription", gross: 100000, debit: 1000000, commission: 5000, net: 95000},
		{name: "converted no bonus", source: "subscription", gross: 100000, debit: 100000, granted: 100000, paid: 100000, share: 6000, commission: 5000, net: 95000},
		{name: "platform bears all bonus", source: "subscription", gross: 110000, debit: 110000, granted: 110000, paid: 100000, share: 0, commission: 5500, discount: 10000, subsidy: 10000, net: 104500},
		{name: "fee rounds from exact ratio", source: "subscription", gross: 6, debit: 6, granted: 110, paid: 100, share: 6000, discount: 1, subsidy: 1, net: 6},
		{name: "fee half unit rounds up", source: "subscription", gross: 5, debit: 5, granted: 120, paid: 100, share: 6000, fee: 1, discount: 1, net: 4},
		{name: "large intermediates", source: "subscription", gross: 8800000000000000000, debit: 8800000000000000000, granted: 8800000000000000000, paid: 8000000000000000000, share: 6000, commission: 440000000000000000, fee: 480000000000000000, discount: 800000000000000000, subsidy: 320000000000000000, net: 7880000000000000000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := openSettlementTestDB(t)
			require.NoError(t, Record(RecordParams{
				RequestID: test.name, GroupID: "group", OwnerUserID: 10, ConsumerUserID: 20,
				BillingSource: test.source, ConsumerDebitAmount: test.debit, SettlementGrossAmount: test.gross,
				CreditPackGrantedQuota: test.granted, CreditPackPaidQuota: test.paid, CreditPackChannelShareBPS: test.share,
			}))
			var item marketplaceschema.Settlement
			require.NoError(t, db.First(&item, "request_id = ?", test.name).Error)
			require.Equal(t, test.commission, item.PlatformCommission)
			require.Equal(t, test.fee, item.TransactionFee)
			require.Equal(t, test.discount, item.CreditPackDiscountAmount)
			require.Equal(t, test.subsidy, item.CreditPackPlatformSubsidyAmount)
			require.Equal(t, test.net, item.OwnerNetAmount)
			require.Equal(t, test.debit, item.ConsumerAmount)
			require.Equal(t, test.gross, item.PlatformCommission+item.TransactionFee+item.OwnerNetAmount)
		})
	}
}

func TestRecordRejectsInvalidCreditPackSnapshotsWithoutFinancialWrites(t *testing.T) {
	tests := []struct {
		name                 string
		granted, paid, share int64
	}{
		{name: "missing snapshot"},
		{name: "missing granted", paid: 100, share: 6000},
		{name: "missing paid", granted: 110, share: 6000},
		{name: "negative granted", granted: -110, paid: 100, share: 6000},
		{name: "negative paid", granted: 110, paid: -100, share: 6000},
		{name: "paid exceeds granted", granted: 100, paid: 110, share: 6000},
		{name: "negative share", granted: 110, paid: 100, share: -1},
		{name: "share exceeds 100 percent", granted: 110, paid: 100, share: 10001},
		{name: "negative owner payout", granted: 100000, paid: 1, share: 10000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := openSettlementTestDB(t)
			require.Error(t, Record(RecordParams{
				RequestID: test.name, GroupID: "group", OwnerUserID: 10, ConsumerUserID: 20,
				BillingSource: "credit_pack", ConsumerDebitAmount: 110000, SettlementGrossAmount: 110000,
				CreditPackGrantedQuota: test.granted, CreditPackPaidQuota: test.paid, CreditPackChannelShareBPS: test.share,
			}))
			for _, model := range []any{&marketplaceschema.Settlement{}, &billingschema.BillingAccount{}, &billingschema.BillingLedgerEntry{}} {
				var count int64
				require.NoError(t, db.Model(model).Count(&count).Error)
				require.Zero(t, count)
			}
		})
	}
}
