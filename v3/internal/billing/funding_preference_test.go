package billing

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestFundingPreferenceValidationAndCanonicalOrder(t *testing.T) {
	for _, tc := range []struct {
		mode  string
		order []string
		want  string
	}{
		{"", nil, "subscription_first"}, {"wallet_first", nil, "wallet_first"},
		{"subscription_first", []string{"wallet", "subscription"}, "wallet_first"},
		{"wallet_first", []string{"subscription"}, "subscription_only"},
		{"subscription_only", []string{"wallet"}, "wallet_only"},
	} {
		got, err := normalizeFundingPreference(tc.mode, tc.order)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %s %v", tc, got, err)
		}
	}
	for _, tc := range []struct {
		mode  string
		order []string
	}{
		{"unknown", nil}, {"unknown", []string{"wallet"}}, {"", []string{"wallet", "wallet"}},
		{"", []string{"points"}}, {"", []string{"wallet", "subscription", "wallet"}},
	} {
		if _, err := normalizeFundingPreference(tc.mode, tc.order); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}

func TestFundingPreferenceSpecificSubscriptionIDsAndNoSource(t *testing.T) {
	now := time.Now()
	req, snap := sourceFixture(now)
	profile := snap.AccountProfiles[7]
	profile.Subscriptions[0].SubscriptionID = 9007199254740993
	profile.Subscriptions = append(profile.Subscriptions,
		catalog.SubscriptionBucket{SubscriptionID: 8, AccountID: 44, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour)},
		catalog.SubscriptionBucket{SubscriptionID: 9, AccountID: 45, StartsAt: now.Add(-time.Hour), ExpiresAt: now})
	profile.SubscriptionOrderIDs = []int64{9, 8, 9007199254740993, 8}
	if got := orderedProfileSources(profile, now); !reflect.DeepEqual(got, []int64{44, 43}) {
		t.Fatalf("sources=%v", got)
	}
	profile.BillingPreference, profile.Subscriptions = "subscription_only", nil
	snap.AccountProfiles[7] = profile
	s := &Settler{}
	_, _, _, err := s.selectFunding(context.Background(), req, snap, nil, profile, now)
	if !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("source only without sources=%v", err)
	}
}
