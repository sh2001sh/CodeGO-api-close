package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestAccountProfileExpiresLocally(t *testing.T) {
	now := time.Unix(10000, 0)
	p := AccountProfile{MultiplierPPM: 500000, MultiplierExpiresAt: now.Add(time.Minute), Subscriptions: []SubscriptionBucket{
		{AccountID: 1, StartsAt: now.Add(-time.Hour), ExpiresAt: now},
		{AccountID: 2, StartsAt: now, ExpiresAt: now.Add(time.Hour)},
		{AccountID: 3, StartsAt: now.Add(time.Second), ExpiresAt: now.Add(time.Hour)},
	}}
	if got := p.CardMultiplier(now); got != 0.5 {
		t.Fatalf("active multiplier %v", got)
	}
	if got := p.CardMultiplier(now.Add(time.Minute)); got != 1 {
		t.Fatalf("expired multiplier %v", got)
	}
	if got := p.SubscriptionAccounts(now); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("subscription windows %v", got)
	}
	if got := p.SubscriptionAccounts(now.Add(time.Second)); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Fatalf("subscription start %v", got)
	}
}

// Regression: expiry of the cheapest card must immediately reveal the next
// eligible card, even when the background expiry worker has not run yet.
func TestAccountProfileSelectsNextUnexpiredCard(t *testing.T) {
	now := time.Unix(10000, 0)
	p := AccountProfile{MultiplierPPM: 500000, MultiplierExpiresAt: now.Add(time.Minute), Cards: []MultiplierCard{
		{MultiplierPPM: 750000, ExpiresAt: now.Add(time.Hour)},
		{MultiplierPPM: 500000, ExpiresAt: now.Add(time.Minute)},
		{MultiplierPPM: 100000, ExpiresAt: now},
		{MultiplierPPM: -1, ExpiresAt: now.Add(time.Hour)},
		{MultiplierPPM: 100000, ExpiresAt: now.Add(time.Hour), ID: 17, PropType: "monthly_pass_multiplier", MaxDiscountMicro: 100, UsedDiscountMicro: 100},
	}}
	if got := p.CardMultiplier(now); got != 0.5 {
		t.Fatalf("best active card %v", got)
	}
	if got := p.CardMultiplier(now.Add(time.Minute)); got != 0.75 {
		t.Fatalf("next card at expiry %v", got)
	}
	if got := p.CardMultiplier(now.Add(time.Hour)); got != 1 {
		t.Fatalf("expired cards still apply %v", got)
	}
	blob, err := json.Marshal(wireSnapshot{AccountProfiles: map[int64]AccountProfile{1: p}})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := decodeWireSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := openSnapshot(wire, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.AccountProfiles[1].CardMultiplier(now.Add(time.Minute)); got != 0.75 {
		t.Fatalf("cards lost in Redis wire format: %v", got)
	}
	card := snap.AccountProfiles[1].Cards[4]
	if card.ID != 17 || card.PropType != "monthly_pass_multiplier" || card.MaxDiscountMicro != 100 || card.UsedDiscountMicro != 100 {
		t.Fatal("card budget metadata lost in snapshot")
	}
}
