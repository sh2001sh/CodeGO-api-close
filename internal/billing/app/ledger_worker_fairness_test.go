package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestLedgerWorkerPoisonAccountsDoNotStarveReferralSettlement(t *testing.T) {
	for _, validAttempts := range []int{0, 2} {
		t.Run(fmt.Sprintf("valid_attempts_%d", validAttempts), func(t *testing.T) {
			db, now := referralTestDB(t)
			ctx := context.Background()
			poisonIDs := make([]string, ledgerWorkerAccountBatchSize)
			poisonEvents := make([]billingschema.BillingOutboxEvent, len(poisonIDs))
			for i := range poisonIDs {
				poisonIDs[i] = fmt.Sprintf("fair-%d-poison-%02d", validAttempts, i)
				poisonEvents[i] = billingschema.BillingOutboxEvent{
					AccountID: poisonIDs[i], AggregateID: "missing-settlement",
					EventType: "billing.reservation_settled", IdempotencyKey: poisonIDs[i],
					CreatedAt: time.Unix(now-3600, 0).UTC(),
				}
			}
			require.NoError(t, db.Create(&poisonEvents).Error)
			accountID := referralPaidWallet(t, db, 1000, 800)
			settlement := referralSettle(t, db, accountID, "fair-valid", "relay:wallet:fair-valid", 100, 100, now)
			require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).
				Where("account_id = ?", accountID).Update("attempts", validAttempts).Error)

			published := false
			for round := 0; round < validAttempts+2; round++ {
				// Fresh events on a broken account must not erase its retry history.
				for _, id := range poisonIDs {
					require.NoError(t, db.Create(&billingschema.BillingOutboxEvent{
						AccountID: id, EventType: "billing.account_credited",
						IdempotencyKey: fmt.Sprintf("%s-fresh-%d", id, round),
					}).Error)
				}
				_, err := RunLedgerWorkerBatch(ctx, ledgerWorkerAccountBatchSize)
				require.NoError(t, err)
				var event billingschema.BillingOutboxEvent
				require.NoError(t, db.First(&event, "aggregate_id = ?", settlement.SettlementID).Error)
				if event.Status == billingschema.BillingOutboxStatusPublished {
					published = true
					break
				}
			}
			require.True(t, published, "24 failing accounts must not starve a later valid settlement, even when fresh events keep arriving")
			var receipt billingschema.ReferralConsumptionRebate
			require.NoError(t, db.First(&receipt, "settlement_id = ?", settlement.SettlementID).Error)
			require.EqualValues(t, 80, receipt.EligibleQuota)
			require.EqualValues(t, 2, receipt.RebateQuota)
			var inviter identityschema.User
			require.NoError(t, db.First(&inviter, 1).Error)
			require.Equal(t, 2, inviter.ClaudeQuota)

			var blocked []billingschema.BillingOutboxEvent
			require.NoError(t, db.Where("account_id IN ?", poisonIDs).Find(&blocked).Error)
			for _, event := range blocked {
				require.Equal(t, billingschema.BillingOutboxStatusPending, event.Status)
				require.Nil(t, event.PublishedAt, "failed account transactions must retain all events")
			}
			var failed billingschema.BillingOutboxEvent
			require.NoError(t, db.First(&failed, "event_id = ?", poisonEvents[0].EventID).Error)
			require.Positive(t, failed.Attempts)
			require.NotEmpty(t, failed.LastError)

			// Repairing the cause makes the retained account publishable on retry.
			require.NoError(t, db.Model(&failed).Update("event_type", "billing.account_credited").Error)
			_, err := RunLedgerWorkerBatch(ctx, len(poisonIDs)+2)
			require.NoError(t, err)
			require.NoError(t, db.First(&failed, "event_id = ?", failed.EventID).Error)
			require.Equal(t, billingschema.BillingOutboxStatusPublished, failed.Status)
			require.Empty(t, failed.LastError)
			require.NotNil(t, failed.PublishedAt)
		})
	}
}
