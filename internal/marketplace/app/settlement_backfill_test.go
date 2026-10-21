package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	"github.com/stretchr/testify/require"
)

func TestBackfillCreditPackUsesOnlyFrozenPurchaseAndPolicy(t *testing.T) {
	db, logDB := openOwnerUsageLogTestDB(t)
	require.NoError(t, db.AutoMigrate(&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingOutboxEvent{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &commerceschema.UserSubscription{}))
	internalID := 101
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: "channel", OwnerUserID: 10, InternalChannelID: &internalID}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "group", ChannelID: "channel", OwnerUserID: 10, Multiplier: 9}).Error)
	require.NoError(t, logDB.Create([]auditschema.Log{
		{RequestId: "new-pack", UserId: 20, ChannelId: internalID, Type: auditschema.LogTypeConsume, Quota: 100000, CreatedAt: time.Now().Unix(),
			Other: `{"marketplace_group_id":"group","billing_source":"subscription","subscription_billing_version":"credit_pack","credit_pack_granted_quota":103000,"credit_pack_paid_quota":100000,"credit_pack_channel_share_bps":6000,"marketplace_fee_policy_version":"paid_bonus_rate_v2","subscription_id":44,"subscription_plan_id":9,"subscription_plan_title":"Frozen pack"}`},
		{RequestId: "missing-snapshot", UserId: 20, ChannelId: internalID, Type: auditschema.LogTypeConsume, Quota: 100000,
			Other: `{"marketplace_group_id":"group","billing_source":"subscription","subscription_billing_version":"credit_pack"}`},
		{RequestId: "old-pack", UserId: 20, ChannelId: internalID, Type: auditschema.LogTypeConsume, Quota: 110000, CreatedAt: time.Now().Unix(),
			Other: `{"marketplace_group_id":"group","billing_source":"subscription","subscription_billing_version":"credit_pack","credit_pack_granted_quota":110000,"credit_pack_paid_quota":100000,"credit_pack_channel_share_bps":6000}`},
	}).Error)
	for i, request := range []string{"new-pack", "old-pack"} {
		granted := int64(103000)
		amount := int64(100000)
		if i == 1 {
			granted, amount = 110000, 110000
		}
		subID := 44 + i
		require.NoError(t, db.Create(&commerceschema.UserSubscription{Id: subID, UserId: 20, CreditPackGrantedQuota: granted, CreditPackPaidQuota: 100000, CreditPackChannelShareBPS: 6000}).Error)
		require.NoError(t, db.Create(&billingschema.BillingAccount{AccountID: request, OwnerType: "user_subscription", OwnerID: int64(subID), AccountType: "subscription", QuotaUnit: "quota"}).Error)
		digest := sha256.Sum256([]byte("credit-pack:" + request))
		require.NoError(t, db.Create(&billingschema.BillingReservation{ReservationID: request, RequestID: fmt.Sprintf("cp:%x", digest[:30]), AccountID: request, Status: "settled", IdempotencyKey: request}).Error)
		require.NoError(t, db.Create(&billingschema.BillingSettlement{ReservationID: request, ActualAmount: amount, Status: "completed", IdempotencyKey: request + ":settle"}).Error)
	}
	dry, err := BackfillMissingSettlements(0, false)
	require.NoError(t, err)
	require.Equal(t, 3, dry.Candidates)
	require.Equal(t, 1, dry.Invalid)
	require.Zero(t, dry.Created)
	report, err := BackfillMissingSettlements(0, true)
	require.NoError(t, err)
	require.Equal(t, 2, report.Created)
	require.Equal(t, 1, report.Invalid)
	var newPack, oldPack marketplaceschema.Settlement
	require.NoError(t, db.First(&newPack, "request_id = ?", "new-pack").Error)
	require.NoError(t, db.First(&oldPack, "request_id = ?", "old-pack").Error)
	require.EqualValues(t, 100000, newPack.SettlementGrossAmount)
	require.EqualValues(t, 5000, newPack.PlatformCommission)
	require.EqualValues(t, 1800, newPack.TransactionFee)
	require.EqualValues(t, 93200, newPack.OwnerNetAmount)
	require.Zero(t, newPack.Multiplier, "current group multiplier must not replace missing credit pack audit evidence")
	require.True(t, newPack.ReferralRebatePending)
	require.EqualValues(t, 6000, oldPack.TransactionFee)
	var entry billingschema.BillingLedgerEntry
	require.NoError(t, db.First(&entry, "idempotency_key = ?", "marketplace-pending:new-pack").Error)
	var snapshot marketplacesettlement.IncomeSnapshot
	require.NoError(t, json.Unmarshal(entry.Metadata, &snapshot))
	require.Equal(t, "Frozen pack", snapshot.SubscriptionPlanTitle)
	require.Equal(t, 44, snapshot.SubscriptionID)
	replay, err := BackfillMissingSettlements(0, true)
	require.NoError(t, err)
	require.Equal(t, 2, replay.Existing)
	require.Zero(t, replay.Created)
}

func TestBackfillDoesNotCreateIncomeFromLogWithoutNativeDebit(t *testing.T) {
	db, logDB := openOwnerUsageLogTestDB(t)
	require.NoError(t, db.AutoMigrate(&billingschema.BillingReservation{}))
	internalID := 101
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: "channel", OwnerUserID: 10, InternalChannelID: &internalID}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "group", ChannelID: "channel", OwnerUserID: 10}).Error)
	require.NoError(t, logDB.Create(&auditschema.Log{RequestId: "unpaid", UserId: 20, ChannelId: internalID, Type: auditschema.LogTypeConsume, Quota: 100000, CreatedAt: time.Now().Unix(), Other: `{"marketplace_group_id":"group","marketplace_multiplier":1.5,"billing_source":"wallet"}`}).Error)
	report, err := BackfillMissingSettlements(0, true)
	require.NoError(t, err)
	require.Zero(t, report.Created)
	require.Equal(t, 1, report.Skipped)
	var count int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestSettlementBackfillBatchFitsPostgresParameterLimit(t *testing.T) {
	const postgresExtendedProtocolParameterLimit = 65535
	if settlementBackfillBatchSize <= 0 || settlementBackfillBatchSize >= postgresExtendedProtocolParameterLimit {
		t.Fatalf("settlement backfill batch size %d exceeds PostgreSQL protocol capacity", settlementBackfillBatchSize)
	}
}

func TestSettlementBackfillGross(t *testing.T) {
	for _, test := range []struct {
		name   string
		quota  int64
		source string
		want   int64
	}{
		{name: "wallet", quota: 1234, source: "wallet", want: 1234},
		{name: "subscription rounds down", quota: 124, source: "subscription", want: 12},
		{name: "subscription rounds half up", quota: 125, source: "subscription", want: 13},
		{name: "small subscription has no gross", quota: 1, source: "subscription", want: 0},
		{name: "non positive", quota: 0, source: "wallet", want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := settlementBackfillGross(test.quota, test.source); got != test.want {
				t.Fatalf("settlementBackfillGross(%d, %q) = %d, want %d", test.quota, test.source, got, test.want)
			}
		})
	}
}
