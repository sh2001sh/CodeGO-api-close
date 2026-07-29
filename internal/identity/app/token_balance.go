package app

import (
	"math"

	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
)

// TokenBalanceSubscription is the minimal active-plan data safe for API-key clients.
type TokenBalanceSubscription struct {
	Name         string  `json:"name"`
	RemainingUSD float64 `json:"remaining_usd"`
	ExpiresAt    int64   `json:"expires_at"`
}

// TokenBalanceSummary contains the account funds usable through one API key.
type TokenBalanceSummary struct {
	Currency                 string                     `json:"currency"`
	AvailableUSD             float64                    `json:"available_usd"`
	AccountAvailableUSD      float64                    `json:"account_available_usd"`
	WalletAvailableUSD       float64                    `json:"wallet_available_usd"`
	SubscriptionAvailableUSD float64                    `json:"subscription_available_usd"`
	TokenAvailableUSD        *float64                   `json:"token_available_usd,omitempty"`
	TokenUnlimited           bool                       `json:"token_unlimited"`
	Subscriptions            []TokenBalanceSubscription `json:"subscriptions"`
}

// BuildTokenBalanceSummary calculates the balance visible to an authenticated API token.
func BuildTokenBalanceSummary(userID int, tokenID int) (*TokenBalanceSummary, error) {
	token, err := GetUserToken(userID, tokenID)
	if err != nil {
		return nil, err
	}
	user, err := LoadUserByID(userID, false)
	if err != nil {
		return nil, err
	}
	walletQuota, _, err := loadDisplayWalletQuotas(user)
	if err != nil {
		return nil, err
	}
	snapshots, err := buildDesktopSubscriptionSnapshots(userID)
	if err != nil {
		return nil, err
	}

	walletAvailable := math.Max(0, quotaToUSD(walletQuota))
	subscriptionAvailable := 0.0
	subscriptions := make([]TokenBalanceSubscription, 0, len(snapshots))
	for _, item := range snapshots {
		remaining := math.Max(0, item.RemainingUSD)
		subscriptionAvailable += remaining
		subscriptions = append(subscriptions, TokenBalanceSubscription{
			Name:         item.PlanTitle,
			RemainingUSD: remaining,
			ExpiresAt:    item.EndTime,
		})
	}

	accountAvailable := walletAvailable + subscriptionAvailable
	summary := &TokenBalanceSummary{
		Currency:                 "USD",
		AvailableUSD:             accountAvailable,
		AccountAvailableUSD:      accountAvailable,
		WalletAvailableUSD:       walletAvailable,
		SubscriptionAvailableUSD: subscriptionAvailable,
		TokenUnlimited:           token.UnlimitedQuota,
		Subscriptions:            subscriptions,
	}
	applyTokenBalanceLimit(summary, token)
	return summary, nil
}

func applyTokenBalanceLimit(summary *TokenBalanceSummary, token *identityschema.Token) {
	if token == nil || token.UnlimitedQuota {
		return
	}
	tokenAvailable := math.Max(0, quotaToUSD(token.RemainQuota))
	summary.TokenAvailableUSD = &tokenAvailable
	summary.AvailableUSD = math.Min(summary.AvailableUSD, tokenAvailable)
}
