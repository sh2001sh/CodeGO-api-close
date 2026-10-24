package domain

import (
	"math"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
)

func NormalizeWalletType(walletType string) string {
	return commerceschema.WalletTypeClaude
}

func IsClaudeWalletType(walletType string) bool {
	return NormalizeWalletType(walletType) == commerceschema.WalletTypeClaude
}

func ApplyDiscountRateToMoney(amount float64, discountRate float64) float64 {
	if amount <= 0 {
		return 0
	}
	if discountRate <= 0 {
		return math.Round(amount*100) / 100
	}
	if discountRate >= 1 {
		discountRate = 0.99
	}
	discounted := math.Round(amount*(1-discountRate)*100) / 100
	if discounted < 0.01 {
		return 0.01
	}
	return discounted
}

// CappedDiscountRate returns the effective rate after limiting the discount
// against a value expressed in the same units as amount.
func CappedDiscountRate(amount float64, discountRate float64, maximumDiscount float64) float64 {
	if amount <= 0 || discountRate <= 0 {
		return 0
	}
	if maximumDiscount <= 0 {
		return discountRate
	}
	return math.Min(discountRate, maximumDiscount/amount)
}

func ApplyDiscountRateToMoneyWithCap(amount float64, discountRate float64, maximumDiscount float64) float64 {
	return ApplyDiscountRateToMoney(amount, CappedDiscountRate(amount, discountRate, maximumDiscount))
}
