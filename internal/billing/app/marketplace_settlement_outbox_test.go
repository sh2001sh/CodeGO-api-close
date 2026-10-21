package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func marketplaceOutboxFixture(t *testing.T, subscription bool) (*gorm.DB, *BillingSession, string) {
	t.Helper()
	db, _ := referralTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))
	channelID := 101
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: "channel", OwnerUserID: 10, InternalChannelID: &channelID}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "group", ChannelID: "channel", OwnerUserID: 10}).Error)
	ctx, info := walletSequenceRequest(context.Background(), 2, "marketplace-outbox")
	info.MarketplaceGroupID, info.MarketplaceOwnerID, info.MarketplaceMultiplier = "group", 10, 1.5
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: channelID}
	if !subscription {
		accountID := referralPaidWallet(t, db, 10000, 10000)
		session, apiErr := NewBillingSession(ctx, info, 1000)
		require.Nil(t, apiErr)
		return db, session, accountID
	}
	require.NoError(t, db.Create(&commerceschema.UserSubscription{Id: 100, UserId: 2, AmountTotal: 10000}).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: 100})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 10000, IdempotencyKey: "subscription-credit"})
	require.NoError(t, err)
	reservation, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, RequestID: info.RequestId, ReservedAmount: 1000, IdempotencyKey: "subscription-reserve"})
	require.NoError(t, err)
	previous := subscriptionFundingHooks
	t.Cleanup(func() { subscriptionFundingHooks = previous })
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{SettleReservationTx: func(tx *gorm.DB, request string, _ int, _ string, amount int64) error {
		_, err := billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{ReservationID: reservation.ReservationID, ActualAmount: amount, UsageEvidenceID: request, IdempotencyKey: "subscription-settle"})
		return err
	}})
	info.BillingSource = BillingSourceSubscription
	funding := &SubscriptionFunding{requestID: info.RequestId, userID: 2, subscriptionID: 100, preConsumed: 1000, AmountTotal: 10000, reservationID: reservation.ReservationID, accountID: account.AccountID}
	return db, &BillingSession{funding: funding, relayInfo: info, preConsumedQuota: 1000, tokenConsumed: 1000}, account.AccountID
}

func TestMarketplaceIntentCommitsAtomicallyWithNativeDebit(t *testing.T) {
	for _, subscription := range []bool{false, true} {
		name := "wallet"
		if subscription {
			name = "legacy_subscription"
		}
		t.Run(name, func(t *testing.T) {
			db, session, accountID := marketplaceOutboxFixture(t, subscription)
			require.NoError(t, db.Exec("CREATE TRIGGER reject_marketplace_intent BEFORE INSERT ON billing_outbox_events WHEN NEW.event_type = 'billing.marketplace_usage_settled' BEGIN SELECT RAISE(ABORT, 'intent unavailable'); END").Error)
			require.ErrorContains(t, session.Settle(1000), "intent unavailable")
			var count int64
			require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Count(&count).Error)
			require.Zero(t, count, "a debit must not commit without its recovery intent")
			var snapshot billingschema.BillingBalanceSnapshot
			require.NoError(t, db.First(&snapshot, "account_id = ?", accountID).Error)
			require.EqualValues(t, 1000, snapshot.ReservedBalance)
			require.NoError(t, db.Exec("DROP TRIGGER reject_marketplace_intent").Error)
			require.NoError(t, session.Settle(1000))
			var event billingschema.BillingOutboxEvent
			require.NoError(t, db.First(&event, "event_type = ?", marketplaceUsageSettledEvent).Error)
			require.Equal(t, accountID, event.AccountID)
			require.Equal(t, session.relayInfo.RequestId, event.AggregateID)
			require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
			// Simulate process exit before the optional inline marketplace write.
			require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Count(&count).Error)
			require.Zero(t, count)
			_, err := processLedgerOutboxAccount(context.Background(), accountID)
			require.NoError(t, err)
			var income marketplaceschema.Settlement
			require.NoError(t, db.First(&income, "request_id = ?", event.AggregateID).Error)
			want := int64(950)
			if subscription {
				want = 95
			}
			require.Equal(t, want, income.OwnerNetAmount)
		})
	}
}

func TestMarketplaceIntentRetriesAfterCreditFailureWithoutDoublePayment(t *testing.T) {
	db, session, accountID := marketplaceOutboxFixture(t, false)
	require.NoError(t, session.Settle(1000))
	require.NoError(t, db.Exec("CREATE TRIGGER reject_marketplace_credit BEFORE INSERT ON billing_ledger_entries WHEN NEW.reason_code = 'marketplace_owner_pending' BEGIN SELECT RAISE(ABORT, 'income unavailable'); END").Error)
	_, err := processLedgerOutboxAccount(context.Background(), accountID)
	require.ErrorContains(t, err, "income unavailable")
	var event billingschema.BillingOutboxEvent
	require.NoError(t, db.First(&event, "event_type = ?", marketplaceUsageSettledEvent).Error)
	require.Equal(t, "pending", event.Status)
	var count int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Exec("DROP TRIGGER reject_marketplace_credit").Error)
	_, err = processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	for _, status := range []string{"pending", "released", "reclaimed", "forfeited"} {
		require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Where("request_id = ?", event.AggregateID).Update("status", status).Error)
		require.NoError(t, db.Model(&event).Update("status", "pending").Error)
		_, err = processLedgerOutboxAccount(context.Background(), accountID)
		require.NoError(t, err)
		var income marketplaceschema.Settlement
		require.NoError(t, db.First(&income, "request_id = ?", event.AggregateID).Error)
		require.Equal(t, status, income.Status)
	}
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", "marketplace-pending:"+event.AggregateID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestMarketplaceIntentRejectsWrongEventIdentityAndFunding(t *testing.T) {
	for _, change := range []string{"request", "account", "consumer"} {
		t.Run(change, func(t *testing.T) {
			db, session, _ := marketplaceOutboxFixture(t, false)
			require.NoError(t, session.Settle(1000))
			var event billingschema.BillingOutboxEvent
			require.NoError(t, db.First(&event, "event_type = ?", marketplaceUsageSettledEvent).Error)
			switch change {
			case "request":
				event.AggregateID = "different"
			case "account":
				event.AccountID = "different"
			case "consumer":
				var usage auditschema.Log
				require.NoError(t, json.Unmarshal(event.Payload, &usage))
				usage.UserId = 999
				var err error
				event.Payload, err = json.Marshal(usage)
				require.NoError(t, err)
			}
			require.Error(t, db.Transaction(func(tx *gorm.DB) error { return consumeMarketplaceUsageTx(tx, event) }))
			var count int64
			require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestMarketplaceIntentPublishesZeroIncomeWithoutBlockingNextEvents(t *testing.T) {
	db, session, accountID := marketplaceOutboxFixture(t, true)
	require.NoError(t, session.Settle(4))
	_, err := processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	var event billingschema.BillingOutboxEvent
	require.NoError(t, db.First(&event, "event_type = ?", marketplaceUsageSettledEvent).Error)
	require.Equal(t, "published", event.Status)
	var count int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestMarketplaceOutboxBatchLimitPreservesRemainingEvents(t *testing.T) {
	db, session, accountID := marketplaceOutboxFixture(t, false)
	require.NoError(t, session.Settle(1000))
	for i := 0; i < ledgerWorkerEventBatchSize; i++ {
		_, err := billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: accountID, Amount: 1, IdempotencyKey: fmt.Sprintf("extra-credit:%d", i)})
		require.NoError(t, err)
	}
	var before, after int64
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Where("account_id = ? AND status = ?", accountID, "pending").Count(&before).Error)
	processed, err := processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	require.Equal(t, ledgerWorkerEventBatchSize, processed)
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Where("account_id = ? AND status = ?", accountID, "pending").Count(&after).Error)
	require.EqualValues(t, before-ledgerWorkerEventBatchSize, after)
	processed, err = processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	require.EqualValues(t, after, processed)
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Where("account_id = ? AND status = ?", accountID, "pending").Count(&after).Error)
	require.Zero(t, after)
}
