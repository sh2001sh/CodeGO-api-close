package ledger

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
)

type preferenceLoader struct {
	fundingLoader
	preferences   map[int64]string
	orders        map[int64][]int64
	preferenceErr error
}

func (l *preferenceLoader) FundingPreferences(context.Context) (map[int64]string, map[int64][]int64, error) {
	return l.preferences, l.orders, l.preferenceErr
}

func TestFundingPreferencesRefreshOrderDefaultAndFailure(t *testing.T) {
	now := time.Now()
	loader := &preferenceLoader{fundingLoader: fundingLoader{sources: []FundingSource{
		{UserID: 1, AccountID: 2, ExpiresAt: now.Add(time.Hour)},
		{UserID: 1, AccountID: 3, ExpiresAt: now.Add(2 * time.Hour)},
		{UserID: 1, AccountID: 4, ExpiresAt: now},
	}}, preferences: map[int64]string{1: "wallet_first"}, orders: map[int64][]int64{1: {4, 3, 3}}}
	a := NewFundingAccounts(nil, loader, func() time.Time { return now })
	if _, err := a.FundingPreference(context.Background(), 1); !errors.Is(err, billing.ErrBillingDegraded) {
		t.Fatalf("unready=%v", err)
	}
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	loader.preferences[1] = "wallet_only" // caller cannot mutate the published snapshot
	pref, sources, err := a.FundingSelection(context.Background(), 1)
	if err != nil || pref != "wallet_first" || !reflect.DeepEqual(sources, []int64{3, 2}) {
		t.Fatalf("selection=%s %v %v", pref, sources, err)
	}
	if pref, err = a.FundingPreference(context.Background(), 2); err != nil || pref != "subscription_first" {
		t.Fatalf("default=%s %v", pref, err)
	}
	loader.preferenceErr = errors.New("settings unavailable")
	if err = a.Refresh(context.Background()); err == nil {
		t.Fatal("failure swallowed")
	}
	if _, _, err = a.FundingSelection(context.Background(), 1); !errors.Is(err, billing.ErrBillingDegraded) {
		t.Fatalf("stale selection=%v", err)
	}
	loader.preferenceErr = nil
	loader.preferences[1] = "invalid"
	if err = a.Refresh(context.Background()); err == nil {
		t.Fatal("invalid preference published")
	}
}
