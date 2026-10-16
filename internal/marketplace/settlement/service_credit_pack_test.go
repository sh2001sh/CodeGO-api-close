package settlement

import (
	"testing"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

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
