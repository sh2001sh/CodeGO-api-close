package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestReferralWorkerBypassesGenericPendingWindow(t *testing.T) {
	db, now := referralTestDB(t)
	older := time.Unix(now-3600, 0).UTC()
	noise := make([]billingschema.BillingOutboxEvent, ledgerWorkerCandidateMax+1)
	for index := range noise {
		noise[index] = billingschema.BillingOutboxEvent{
			AccountID:      fmt.Sprintf("noise-%05d", index),
			EventType:      "billing.credit_granted",
			IdempotencyKey: fmt.Sprintf("noise-%05d", index),
			CreatedAt:      older,
		}
	}
	require.NoError(t, db.CreateInBatches(&noise, 500).Error)

	accountID := referralPaidWallet(t, db, 1000, 800)
	settlement := referralSettle(t, db, accountID, "dedicated-referral", "relay:wallet:dedicated-referral", 100, 100, now)

	accountIDs, err := pendingReferralOutboxAccountIDs(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, []string{accountID}, accountIDs,
		"generic pending events must not hide a later referral settlement")

	processed, err := RunReferralLedgerWorkerBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, processed)

	var event billingschema.BillingOutboxEvent
	require.NoError(t, db.First(&event, "aggregate_id = ?", settlement.SettlementID).Error)
	require.Equal(t, billingschema.BillingOutboxStatusPublished, event.Status)
	var receipt billingschema.ReferralConsumptionRebate
	require.NoError(t, db.First(&receipt, "settlement_id = ?", settlement.SettlementID).Error)
	require.EqualValues(t, 80, receipt.EligibleQuota)
	require.EqualValues(t, 2, receipt.RebateQuota)
	var inviter identityschema.User
	require.NoError(t, db.First(&inviter, 1).Error)
	require.Equal(t, 2, inviter.ClaudeQuota)
}

func TestMarketplaceReferralWorkerBypassesNonContextSettlements(t *testing.T) {
	db, now := referralTestDB(t)
	older := time.Unix(now-3600, 0).UTC()
	noise := make([]billingschema.BillingOutboxEvent, 1000)
	for index := range noise {
		noise[index] = billingschema.BillingOutboxEvent{
			AccountID: fmt.Sprintf("settlement-noise-%04d", index), AggregateID: fmt.Sprintf("missing-%04d", index),
			EventType: "billing.reservation_settled", IdempotencyKey: fmt.Sprintf("settlement-noise-%04d", index),
			CreatedAt: older,
		}
	}
	require.NoError(t, db.CreateInBatches(&noise, 500).Error)

	accountID := referralPaidWallet(t, db, 1000, 800)
	settlement := referralSettle(t, db, accountID, "marketplace-priority", "relay:wallet:marketplace-priority", 100, 100, now)
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).
		Where("aggregate_id = ?", settlement.SettlementID).
		Update("payload", json.RawMessage(`{"referral_context":{}}`)).Error)

	accountIDs, err := pendingMarketplaceReferralOutboxAccountIDs(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, []string{accountID}, accountIDs)
	processed, err := RunMarketplaceReferralLedgerWorkerBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, processed)

	var event billingschema.BillingOutboxEvent
	require.NoError(t, db.First(&event, "aggregate_id = ?", settlement.SettlementID).Error)
	require.Equal(t, billingschema.BillingOutboxStatusPublished, event.Status)
	var noisePending int64
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).
		Where("idempotency_key LIKE ? AND status = ?", "settlement-noise-%", billingschema.BillingOutboxStatusPending).
		Count(&noisePending).Error)
	require.EqualValues(t, len(noise), noisePending)
}
