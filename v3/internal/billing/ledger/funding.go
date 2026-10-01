package ledger

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
)

// FundingSource is a currently active subscription, supplied by commerce.
type FundingSource = billing.FundingSource

type FundingSourceLoader interface {
	ActiveFundingSources(context.Context) ([]FundingSource, error)
}

type FundingPreferenceLoader interface {
	FundingPreferences(context.Context) (map[int64]string, map[int64][]int64, error)
}

type fundingSnapshot struct {
	users       map[int64][]FundingSource
	preferences map[int64]string
}

// FundingAccounts adds subscription profiles to the normal wallet/balance
// reader. Refresh is called before readiness and then from a worker loop; the
// gateway only reads this immutable local snapshot.
type FundingAccounts struct {
	*Accounts
	loader   FundingSourceLoader
	now      func() time.Time
	snapshot atomic.Pointer[fundingSnapshot]
}

func NewFundingAccounts(accounts *Accounts, loader FundingSourceLoader, now func() time.Time) *FundingAccounts {
	if now == nil {
		now = time.Now
	}
	return &FundingAccounts{Accounts: accounts, loader: loader, now: now}
}

func (a *FundingAccounts) Refresh(ctx context.Context) error {
	sources, err := a.loader.ActiveFundingSources(ctx)
	if err != nil {
		a.snapshot.Store(nil) // stale membership must never become wallet-only admission
		return err
	}
	snapshot := &fundingSnapshot{users: make(map[int64][]FundingSource), preferences: make(map[int64]string)}
	orders := map[int64][]int64{}
	if loader, ok := a.loader.(FundingPreferenceLoader); ok {
		var preferences map[int64]string
		preferences, orders, err = loader.FundingPreferences(ctx)
		if err == nil {
			for user, preference := range preferences {
				switch preference {
				case "", "subscription_first", "wallet_first", "subscription_only", "wallet_only":
					snapshot.preferences[user] = preference
				default:
					err = fmt.Errorf("billing: invalid funding preference")
				}
			}
		}
		if err != nil {
			a.snapshot.Store(nil)
			return err
		}
	}
	for _, source := range sources {
		if source.UserID > 0 && source.AccountID > 0 && source.ExpiresAt.After(a.now()) {
			snapshot.users[source.UserID] = append(snapshot.users[source.UserID], source)
		}
	}
	for user, list := range snapshot.users {
		sort.Slice(list, func(i, j int) bool {
			if list[i].ExpiresAt.Equal(list[j].ExpiresAt) {
				return list[i].AccountID < list[j].AccountID
			}
			return list[i].ExpiresAt.Before(list[j].ExpiresAt)
		})
		priorities := make(map[int64]int, len(orders[user]))
		for _, account := range orders[user] {
			if _, exists := priorities[account]; !exists {
				priorities[account] = len(priorities) + 1
			}
		}
		sort.SliceStable(list, func(i, j int) bool {
			left, right := priorities[list[i].AccountID], priorities[list[j].AccountID]
			return left > 0 && (right == 0 || left < right)
		})
	}
	a.snapshot.Store(snapshot)
	return nil
}

func (a *FundingAccounts) FundingSelection(_ context.Context, userID int64) (string, []int64, error) {
	snapshot := a.snapshot.Load()
	if snapshot == nil {
		return "", nil, billing.ErrBillingDegraded
	}
	var result []int64
	for _, source := range snapshot.users[userID] {
		if source.ExpiresAt.After(a.now()) {
			result = append(result, source.AccountID)
		}
	}
	preference := snapshot.preferences[userID]
	if preference == "" {
		preference = "subscription_first"
	}
	return preference, result, nil
}

func (a *FundingAccounts) SubscriptionAccounts(ctx context.Context, userID int64) ([]int64, error) {
	_, accounts, err := a.FundingSelection(ctx, userID)
	return accounts, err
}

func (a *FundingAccounts) FundingPreference(ctx context.Context, userID int64) (string, error) {
	preference, _, err := a.FundingSelection(ctx, userID)
	return preference, err
}

// Run refreshes profiles every second; expired subscriptions are also filtered
// by request time, so an idle gateway cannot admit against an expired bucket.
func (a *FundingAccounts) Run(ctx context.Context) error {
	if err := a.Refresh(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := a.Refresh(ctx); err != nil {
				return err
			}
		}
	}
}
