package app

import (
	"sync"
	"testing"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/workflow/temporal/contracts"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
	temporalclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"gorm.io/gorm"
)

func TestRequestSettlementProjectionKeepsNativeFundingIdentityAndAcceptedAmount(t *testing.T) {
	for _, test := range []struct {
		name, preference, source      string
		wallet, pre, actual, accepted int
		limitedToken                  bool
	}{
		{name: "wallet", preference: "wallet_only", source: "wallet", wallet: 10000, pre: 100, actual: 160, accepted: 160},
		{name: "wallet_exhausted_cap", preference: "wallet_only", source: "wallet", wallet: 50, pre: 10, actual: 100, accepted: 50},
		{name: "legacy_multiplier", preference: "subscription_only", source: "legacy", pre: 100, actual: 160, accepted: 40},
		{name: "new_pack_priority", preference: "subscription_first", source: "pack", pre: 100, actual: 160, accepted: 160},
		{name: "pack_refund_then_legacy", preference: "subscription_only", source: "legacy", pre: 100, actual: 160, accepted: 40, limitedToken: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, info := creditPackPriorityContext(t, test.preference, test.wallet)
			info.ChannelId = 123
			if test.limitedToken {
				seedToken(t, info.UserId+1000000, info.UserId, "projection-limited", 50)
				info.TokenId, info.TokenKey, info.IsPlayground = info.UserId+1000000, "projection-limited", false
				previousSQLite := platformdb.UsingSQLite
				platformdb.UsingSQLite = false
				t.Cleanup(func() { platformdb.UsingSQLite = previousSQLite })
			}
			var legacyCalls, packCalls, refunds int
			reservations := make(map[string]*billingschema.BillingReservation)
			reserve := func(request string, subscriptionID int, amount int64, pack bool) (*SubscriptionFundingPreConsumeResult, error) {
				account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(subscriptionID)})
				if err != nil {
					return nil, err
				}
				if _, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 10000, IdempotencyKey: "projection-credit:" + request}); err != nil {
					return nil, err
				}
				reservation, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, RequestID: request, ReservedAmount: amount, IdempotencyKey: "subscription:" + request + ":reserve"})
				if err != nil {
					return nil, err
				}
				reservations[request] = reservation
				result := &SubscriptionFundingPreConsumeResult{UserSubscriptionID: subscriptionID, PreConsumed: amount, ReservationID: reservation.ReservationID, AccountID: account.AccountID, AmountTotal: 10000, PlanID: subscriptionID}
				if pack {
					result.CreditPackGrantedQuota, result.CreditPackPaidQuota, result.CreditPackChannelShareBPS = 103000, 100000, 6000
				}
				return result, nil
			}
			RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
				PreConsumeCreditPack: func(request string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
					packCalls++
					if test.source == "legacy" && !test.limitedToken {
						return nil, billingdomain.ErrInsufficientBalance
					}
					return reserve(request, info.UserId+2000000, amount, true)
				},
				PreConsume: func(request string, _ int, _ string, amount int64) (*SubscriptionFundingPreConsumeResult, error) {
					legacyCalls++
					return reserve(request, info.UserId+3000000, amount, false)
				},
				SettleReservation: func(request string, _ int, _ string, amount int64) error {
					_, err := billingdomain.SettleReservation(billingdomain.SettleReservationParams{ReservationID: reservations[request].ReservationID, ActualAmount: amount, UsageEvidenceID: request, IdempotencyKey: "subscription:" + request + ":settle"})
					return err
				},
				SettleReservationTx: func(tx *gorm.DB, request string, _ int, _ string, amount int64) error {
					_, err := billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{ReservationID: reservations[request].ReservationID, ActualAmount: amount, UsageEvidenceID: request, IdempotencyKey: "subscription:" + request + ":settle"})
					return err
				},
				RefundPreConsume: func(request string) error {
					refunds++
					_, err := billingdomain.ReleaseReservation(billingdomain.ReleaseReservationParams{ReservationID: reservations[request].ReservationID, IdempotencyKey: "subscription:" + request + ":refund"})
					return err
				},
			})
			projections := make(chan contracts.RequestSettlementWorkflowInput, 2)
			client := &mocks.Client{}
			capture := func(arguments mock.Arguments) {
				options := arguments.Get(1).(temporalclient.StartWorkflowOptions)
				input := arguments.Get(3).(contracts.RequestSettlementWorkflowInput)
				require.Equal(t, "request-settlement-"+input.RequestID, options.ID)
				projections <- input
			}
			client.On("ExecuteWorkflow", mock.Anything, mock.Anything, contracts.WorkflowRequestSettlement, mock.Anything).Run(capture).Return(nil, nil).Once()
			client.On("ExecuteWorkflow", mock.Anything, mock.Anything, contracts.WorkflowRequestSettlement, mock.Anything).Run(capture).Return(nil, &serviceerror.WorkflowExecutionAlreadyStarted{}).Once()
			previousClient, previousErr := requestSettlementTemporalClient, requestSettlementTemporalClientErr
			requestSettlementTemporalClientOnce.Do(func() {})
			requestSettlementTemporalClient, requestSettlementTemporalClientErr = client, nil
			t.Cleanup(func() {
				requestSettlementTemporalClient, requestSettlementTemporalClientErr = previousClient, previousErr
				requestSettlementTemporalClientOnce = sync.Once{}
				if previousClient != nil || previousErr != nil {
					requestSettlementTemporalClientOnce.Do(func() {})
				}
			})
			require.Nil(t, PreConsumeRelayBilling(ctx, test.pre, info))
			require.NoError(t, SettleRelayBilling(ctx, info, test.actual))
			var first contracts.RequestSettlementWorkflowInput
			select {
			case first = <-projections:
			case <-time.After(5 * time.Second):
				t.Fatal("settlement projection was not scheduled")
			}
			var reservation billingschema.BillingReservation
			require.NoError(t, platformdb.DB.First(&reservation, "reservation_id = ?", first.ReservationID).Error)
			var settlement billingschema.BillingSettlement
			require.NoError(t, platformdb.DB.First(&settlement, "settlement_id = ?", first.SettlementID).Error)
			require.Equal(t, reservation.RequestID, first.RequestID, "retained ce worker checks this identity exactly")
			require.Equal(t, reservation.AccountID, first.AccountID)
			require.Equal(t, reservation.ReservationID, settlement.ReservationID)
			require.EqualValues(t, test.accepted, settlement.ActualAmount)
			require.Equal(t, settlement.ActualAmount, first.ActualAmount)
			require.Equal(t, info.RequestId, first.UsageEvidenceID, "HTTP identity remains the public correlation key")
			if test.source == "pack" {
				require.NotEqual(t, info.RequestId, first.RequestID)
				require.Zero(t, legacyCalls)
			} else {
				require.Equal(t, info.RequestId, first.RequestID)
			}
			if test.limitedToken {
				require.Equal(t, 1, refunds)
			}
			require.NoError(t, SettleRelayBilling(ctx, info, test.actual))
			select {
			case replay := <-projections:
				require.Equal(t, first, replay)
			case <-time.After(5 * time.Second):
				t.Fatal("replayed projection was not scheduled")
			}
			var count int64
			require.NoError(t, platformdb.DB.Model(&billingschema.BillingSettlement{}).Where("reservation_id = ?", first.ReservationID).Count(&count).Error)
			require.EqualValues(t, 1, count)
			client.AssertExpectations(t)
			t.Logf("source=%s funding_request=%s HTTP=%s accepted=%d pack_calls=%d legacy_calls=%d refunds=%d", test.source, first.RequestID, first.UsageEvidenceID, first.ActualAmount, packCalls, legacyCalls, refunds)
		})
	}
}
