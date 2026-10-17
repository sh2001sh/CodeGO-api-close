package app

import (
	"encoding/json"
	"testing"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	"github.com/stretchr/testify/require"
)

func TestOwnerIncomeBreakdownUsesFrozenIdentityAndActualReferral(t *testing.T) {
	metadata, err := json.Marshal(marketplacesettlement.IncomeSnapshot{
		AlgorithmVersion: marketplacesettlement.FeePolicyPaidBonusV2, BillingSource: "subscription",
		SubscriptionID: 44, SubscriptionPlanID: 9, SubscriptionPlanTitle: "Original pack",
		BaseCommissionRateBPS: 500, PackageBonusRate: .03, ChannelShareBPS: 6000, PackageSurchargeRate: .018,
	})
	require.NoError(t, err)
	detail := ownerIncomeBreakdown(auditschema.Log{Other: `{"subscription_id":45,"subscription_plan_title":"Renamed pack"}`}, marketplaceschema.Settlement{
		ID: "settlement", BillingSource: "subscription", SettlementGrossAmount: 100000,
		PlatformCommission: 5000, TransactionFee: 1800, ReferralRebateAmount: 3000, OwnerNetAmount: 90200,
		CreditPackGrantedQuota: 103000, CreditPackPaidQuota: 100000, CreditPackChannelShareBPS: 6000,
		IncomeSnapshot: metadata,
	})
	require.Equal(t, "Original pack", *detail.SubscriptionPlanTitle)
	require.Equal(t, 44, *detail.SubscriptionID)
	require.Equal(t, "recorded", detail.SnapshotStatus)
	require.EqualValues(t, 1800, *detail.PackageSurchargeAmount)
	require.EqualValues(t, 3000, *detail.ReferralRebateAmount)
	require.EqualValues(t, 90200, *detail.OwnerNetAmount)
	require.Equal(t, "100000 - 5000 - 1800 - 3000 = 90200", *detail.Formula)
}

func TestOwnerIncomeBreakdownKeepsUnknownHistoryAndRealZeroSeparate(t *testing.T) {
	detail := ownerIncomeBreakdown(auditschema.Log{}, marketplaceschema.Settlement{
		ID: "old", BillingSource: "wallet", SettlementGrossAmount: 100,
		PlatformCommission: 5, OwnerNetAmount: 95,
	})
	require.Nil(t, detail.AlgorithmVersion)
	require.Nil(t, detail.BaseCommissionRateBPS)
	require.Nil(t, detail.SubscriptionID)
	require.Nil(t, detail.PackageSurchargeRate)
	require.NotNil(t, detail.PackageSurchargeAmount)
	require.Zero(t, *detail.PackageSurchargeAmount)
	require.Equal(t, "historical_partial", detail.SnapshotStatus)
	missing := ownerIncomeBreakdown(auditschema.Log{Other: `{"billing_source":"subscription","subscription_id":5,"subscription_plan_title":"Frozen name","subscription_billing_version":"credit_pack"}`}, marketplaceschema.Settlement{})
	require.Equal(t, "Frozen name", *missing.SubscriptionPlanTitle)
	require.Nil(t, missing.SettlementGrossAmount)
	require.Nil(t, missing.OwnerNetAmount)
	require.Nil(t, missing.BaseCommissionAmount)
	require.Nil(t, missing.PackageSurchargeAmount)
	require.Nil(t, missing.Formula)
	item := ownerUsageLogItem(auditschema.Log{Quota: 605, Other: `{"billing_source":"subscription","subscription_billing_version":"credit_pack"}`}, ownerUsageChannel{}, marketplaceschema.Settlement{}, "user")
	require.EqualValues(t, 605, item.ConsumerAmount)
}

func TestOwnerIncomeSnapshotBatchIsOwnerAndLedgerScoped(t *testing.T) {
	db, _ := openOwnerUsageLogTestDB(t)
	for _, item := range []marketplaceschema.Settlement{
		{ID: "mine", RequestID: "request-mine", GroupID: "group", OwnerUserID: 10, PendingAccountID: "account-mine"},
		{ID: "foreign", RequestID: "request-foreign", GroupID: "group", OwnerUserID: 11, PendingAccountID: "account-foreign"},
	} {
		require.NoError(t, db.Create(&item).Error)
	}
	for _, entry := range []billingschema.BillingLedgerEntry{
		{EntryID: "mine", ReferenceType: "marketplace_settlement", ReferenceID: "mine", AccountID: "account-mine", ReasonCode: "marketplace_owner_pending", IdempotencyKey: "marketplace-pending:request-mine", Metadata: json.RawMessage(`{"algorithm_version":"test"}`)},
		{EntryID: "foreign", ReferenceType: "marketplace_settlement", ReferenceID: "foreign", AccountID: "account-foreign", ReasonCode: "marketplace_owner_pending", IdempotencyKey: "marketplace-pending:request-foreign", Metadata: json.RawMessage(`{"subscription_plan_title":"secret"}`)},
	} {
		require.NoError(t, db.Create(&entry).Error)
	}
	items, err := loadOwnerUsageSettlements(10, []auditschema.Log{{RequestId: "request-mine"}, {RequestId: "request-foreign"}})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.JSONEq(t, `{"algorithm_version":"test"}`, string(items["request-mine"].IncomeSnapshot))
}
