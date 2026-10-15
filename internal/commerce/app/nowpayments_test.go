package app

import (
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetNowPaymentsUSDTAmount(t *testing.T) {
	previousQuota := commercestore.NowPaymentsQuotaPerUSDT
	t.Cleanup(func() {
		commercestore.NowPaymentsQuotaPerUSDT = previousQuota
	})

	commercestore.NowPaymentsQuotaPerUSDT = 5
	assert.InDelta(t, 1, GetNowPaymentsUSDTAmount(5), 0.000001)
	assert.InDelta(t, 10, GetNowPaymentsUSDTAmount(50), 0.000001)
}

func TestBuildNowPaymentsPayMethodUsesConfiguredMinimum(t *testing.T) {
	previousMinimum := commercestore.NowPaymentsMinTopUp
	t.Cleanup(func() {
		commercestore.NowPaymentsMinTopUp = previousMinimum
	})

	commercestore.NowPaymentsMinTopUp = 5
	assert.Equal(t, "5", BuildNowPaymentsPayMethod()["min_topup"])
}
