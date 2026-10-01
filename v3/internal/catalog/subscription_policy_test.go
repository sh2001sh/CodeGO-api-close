package catalog

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSubscriptionPolicyExactDecimalAndMissingPolicy(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"default":{"enabled":true,"multiplier":1.2345675,"paid_only":true}}`),
		json.RawMessage(`"{\"default\":{\"enabled\":true,\"multiplier\":1.2345675,\"paid_only\":true}}"`),
	} {
		got, err := ParseSubscriptionPolicies(raw)
		if err != nil || got["default"] != (SubscriptionPolicy{Enabled: true, MultiplierPPM: 1234568, PaidOnly: true}) {
			t.Fatalf("exact policy=%v err=%v", got, err)
		}
		if _, exists := got["vip"]; exists {
			t.Fatal("an absent group acquired an invented subscription policy")
		}
	}
	if got, err := ParseSubscriptionPolicies(nil); got != nil || err != nil {
		t.Fatal("missing setting should remain absent")
	}
	defaults, err := compileSubscriptionPolicies(nil)
	if err != nil || len(defaults) != 3 || !defaults["default"].Enabled || defaults["vip"].MultiplierPPM != 1000000 || defaults["svip"].MultiplierPPM != 1000000 {
		t.Fatal("actual unpersisted v2 runtime defaults were lost")
	}
	explicit, err := compileSubscriptionPolicies(json.RawMessage(`{}`))
	if err != nil || len(explicit) != 0 {
		t.Fatal("explicit empty source policy was replaced by defaults")
	}
	for _, raw := range []string{`null`, `[]`, `{"":{"multiplier":1}}`, `{"default":{"multiplier":0}}`, `{"default":{"multiplier":-1}}`, `{"default":{"multiplier":1e30}}`, `{"default":{"enabled":"true","multiplier":1}}`} {
		if _, err := ParseSubscriptionPolicies(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid policy accepted: %s", raw)
		}
	}
}

func TestSubscriptionSourceMetadataSurvivesWire(t *testing.T) {
	now := time.Unix(10000, 0)
	snap := &Snapshot{SubscriptionPolicies: map[string]SubscriptionPolicy{"paid": {Enabled: true, MultiplierPPM: 250000, PaidOnly: true}},
		AccountProfiles: map[int64]AccountProfile{1: {Subscriptions: []SubscriptionBucket{{AccountID: 10, SubscriptionID: 2, Paid: true, StartsAt: now, ExpiresAt: now.Add(time.Minute), ModelLimits: map[string]int64{"gpt": 50}, ModelUsage: map[string]int64{"gpt": 20}}}}}}
	wire, err := sealSnapshot(snap, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeWireSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := openSnapshot(decoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	bucket := loaded.AccountProfiles[1].Subscriptions[0]
	if !bucket.Paid || bucket.SubscriptionID != 2 || bucket.Models != nil || bucket.ModelLimits["gpt"] != 50 || bucket.ModelUsage["gpt"] != 20 || loaded.SubscriptionPolicies["paid"].MultiplierPPM != 250000 {
		t.Fatal("source policy/provenance/model caps were lost in publication")
	}
}
