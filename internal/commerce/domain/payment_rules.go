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
