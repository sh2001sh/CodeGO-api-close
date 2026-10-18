package app

import (
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	"github.com/stretchr/testify/require"
)

func TestOwnerIncomeFreezeScopesCurrentAdministrativeHolds(t *testing.T) {
	db, _ := openOwnerUsageLogTestDB(t)
	require.NoError(t, db.AutoMigrate(&billingschema.BillingAccount{}, &billingschema.BillingReservation{}))
	for _, account := range []billingschema.BillingAccount{
		{AccountID: "mine", OwnerType: "user", OwnerID: 10, AccountType: "claude_wallet"},
		{AccountID: "foreign", OwnerType: "user", OwnerID: 11, AccountType: "claude_wallet"},
		{AccountID: "subscription", OwnerType: "user", OwnerID: 10, AccountType: "subscription"},
	} {
		require.NoError(t, db.Create(&account).Error)
	}
	for _, r := range []billingschema.BillingReservation{
		{AccountID: "mine", WorkflowID: OwnerIncomeFreezeWorkflow, Status: "open", ReservedAmount: 100, IdempotencyKey: "hold-1", CreatedAt: time.Now().Add(-72 * time.Hour)},
		{AccountID: "mine", WorkflowID: OwnerIncomeFreezeWorkflow, Status: "open", ReservedAmount: 200, IdempotencyKey: "hold-2"},
		{AccountID: "mine", WorkflowID: OwnerIncomeFreezeWorkflow, Status: "released", ReservedAmount: 400, IdempotencyKey: "released"},
		{AccountID: "mine", WorkflowID: OwnerIncomeFreezeWorkflow, Status: "settled", ReservedAmount: 800, IdempotencyKey: "settled"},
		{AccountID: "mine", RequestID: "api-call", Status: "open", ReservedAmount: 1600, IdempotencyKey: "relay"},
		{AccountID: "foreign", WorkflowID: OwnerIncomeFreezeWorkflow, Status: "open", ReservedAmount: 3200, IdempotencyKey: "foreign"},
		{AccountID: "subscription", WorkflowID: OwnerIncomeFreezeWorkflow, Status: "open", ReservedAmount: 6400, IdempotencyKey: "subscription"},
	} {
		require.NoError(t, db.Create(&r).Error)
	}
	result, err := GetOwnerIncomeFreeze(10)
	require.NoError(t, err)
	require.EqualValues(t, 300, result.FrozenAmount)
	result, err = GetOwnerIncomeFreeze(12)
	require.NoError(t, err)
	require.Zero(t, result.FrozenAmount)
	_, err = GetOwnerIncomeFreeze(0)
	require.Error(t, err)
	require.NoError(t, db.Migrator().DropTable(&billingschema.BillingReservation{}))
	_, err = GetOwnerIncomeFreeze(10)
	require.Error(t, err, "database failure must not look like an unfrozen wallet")
}
