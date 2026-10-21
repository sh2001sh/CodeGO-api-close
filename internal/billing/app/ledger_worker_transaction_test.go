package app

import (
	"context"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestLedgerWorkerCommitsSuccessfulEventBeforeLaterFailure(t *testing.T) {
	db, now := referralTestDB(t)
	accountID := referralPaidWallet(t, db, 1000, 1000)
	settlement := referralSettle(t, db, accountID, "short-transaction", "relay:wallet:short-transaction", 100, 100, now)
	failed := billingschema.BillingOutboxEvent{
		AccountID: accountID, AggregateID: "missing-settlement",
		EventType: "billing.reservation_settled", IdempotencyKey: "late-invalid-event",
		CreatedAt: db.NowFunc().Add(time.Minute),
	}
	require.NoError(t, db.Create(&failed).Error)
	processed, err := processLedgerOutboxAccount(context.Background(), accountID)
	require.Error(t, err)
	require.Positive(t, processed, "a later failure must not roll back earlier committed events or retain their account locks: %v", err)
	var successful billingschema.BillingOutboxEvent
	require.NoError(t, db.First(&successful, "idempotency_key = ?", "outbox:"+settlement.IdempotencyKey).Error)
	require.Equal(t, billingschema.BillingOutboxStatusPublished, successful.Status)
	var receipt billingschema.ReferralConsumptionRebate
	require.NoError(t, db.First(&receipt, "settlement_id = ?", settlement.SettlementID).Error)
	require.EqualValues(t, 3, receipt.RebateQuota)
	var inviter identityschema.User
	require.NoError(t, db.First(&inviter, 1).Error)
	require.Equal(t, 3, inviter.ClaudeQuota)
	require.NoError(t, db.First(&failed, "event_id = ?", failed.EventID).Error)
	require.Equal(t, billingschema.BillingOutboxStatusPending, failed.Status)
	require.Nil(t, failed.PublishedAt)

	// Retry must not pay the successful rebate again.
	require.NoError(t, db.Model(&failed).Update("event_type", "billing.account_credited").Error)
	_, err = processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, 1).Error)
	require.Equal(t, 3, inviter.ClaudeQuota)
	var receipts int64
	require.NoError(t, db.Model(&billingschema.ReferralConsumptionRebate{}).Where("settlement_id = ?", settlement.SettlementID).Count(&receipts).Error)
	require.EqualValues(t, 1, receipts)
}
