package app

import (
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	platformgeneral "github.com/sh2001sh/new-api/internal/platform/general"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatWaffoPancakeAmount_UsesDisplayPriceString(t *testing.T) {
	testCases := []struct {
		name     string
		amount   float64
		expected string
	}{
		{name: "whole amount", amount: 29, expected: "29.00"},
		{name: "decimal amount", amount: 29.9, expected: "29.90"},
		{name: "round half up to cents", amount: 29.999, expected: "30.00"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, formatWaffoPancakeAmount(tc.amount))
		})
	}
}

func TestGetWaffoPancakePayMoney(t *testing.T) {
	originalUnitPrice := commercestore.WaffoPancakeUnitPrice
	originalQuotaDisplayType := platformgeneral.GetSetting().QuotaDisplayType
	originalDiscounts := make(map[int]float64, len(commercestore.GetPaymentSetting().AmountDiscount))
	for k, v := range commercestore.GetPaymentSetting().AmountDiscount {
		originalDiscounts[k] = v
	}
	originalTopupGroupRatio := commercedomain.TopupGroupRatioJSON()

	t.Cleanup(func() {
		commercestore.WaffoPancakeUnitPrice = originalUnitPrice
		platformgeneral.GetSetting().QuotaDisplayType = originalQuotaDisplayType
		commercestore.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, commercedomain.UpdateTopupGroupRatio(originalTopupGroupRatio))
	})

	commercestore.WaffoPancakeUnitPrice = 2.5
	commercestore.GetPaymentSetting().AmountDiscount = map[int]float64{
		10:                                    0.8,
		int(platformruntime.QuotaPerUnit * 3): 0.5,
		20:                                    0,
	}
	require.NoError(t, commercedomain.UpdateTopupGroupRatio(`{"default":1,"vip":1.2}`))

	testCases := []struct {
		name             string
		amount           int64
		group            string
		quotaDisplayType string
		expected         float64
	}{
		{
			name:             "currency display applies unit price group ratio and discount",
			amount:           10,
			group:            "vip",
			quotaDisplayType: platformgeneral.QuotaDisplayTypeUSD,
			expected:         24,
		},
		{
			name:             "tokens display converts quota to display units before pricing",
			amount:           int64(platformruntime.QuotaPerUnit * 3),
			group:            "vip",
			quotaDisplayType: platformgeneral.QuotaDisplayTypeTokens,
			expected:         4.5,
		},
		{
			name:             "non-positive discount falls back to no discount",
			amount:           20,
			group:            "default",
			quotaDisplayType: platformgeneral.QuotaDisplayTypeUSD,
			expected:         50,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			platformgeneral.GetSetting().QuotaDisplayType = tc.quotaDisplayType
			actual := GetWaffoPancakePayMoney(tc.amount, tc.group)
			require.InDelta(t, tc.expected, actual, 0.000001)
		})
	}
}
