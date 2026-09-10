package app

import (
	"strings"
	"testing"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLedgerSyncRequestIDFitsReservationColumn(t *testing.T) {
	operationID := "wallet-conversion:" + strings.Repeat("a", 64) + ":debit"

	requestID := ledgerSyncRequestID(operationID)

	require.LessOrEqual(t, len(requestID), billingReservationRequestIDMax)
	require.Equal(t, requestID, ledgerSyncRequestID(operationID))
	require.NotEqual(t, "ledger-sync:"+operationID, requestID)
}

func TestForfeitMarketplacePendingEarningsUsesBoundedRequestID(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 0)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: "marketplace_pending", OwnerType: "marketplace_settlement",
		OwnerID: 99, QuotaUnit: "quota",
	})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: 125, IdempotencyKey: "forfeit-test-fund",
		ReasonCode: "test", ReferenceType: "test", ReferenceID: "pending",
		OperatorType: "test", OperatorID: "forfeit",
	})
	require.NoError(t, err)

	operationID := "marketplace-forfeit:" + strings.Repeat("x", 36)
	require.NoError(t, platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return ForfeitMarketplacePendingEarningsTx(tx, account.AccountID, 1, 125, operationID)
	}))

	var reservation billingschema.BillingReservation
	require.NoError(t, platformdb.DB.First(&reservation).Error)
	require.LessOrEqual(t, len(reservation.RequestID), billingReservationRequestIDMax)
	require.Equal(t, ledgerSyncRequestID(operationID), reservation.RequestID)
}
