package catalog

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestProfileFundingPreferencesPreserveCanonicalOrderAndSourceIDs(t *testing.T) {
	for _, row := range []struct {
		settings string
		mode     string
		order    []string
	}{
		{`{}`, "subscription_first", []string{"subscription", "wallet"}},
		{`{"billing_preference":"wallet_first"}`, "wallet_first", []string{"wallet", "subscription"}},
		{`{"billing_preference":"subscription_only"}`, "subscription_only", []string{"subscription"}},
		{`{"billing_preference":"wallet_only"}`, "wallet_only", []string{"wallet"}},
		{`{"billing_preference":"subscription_first","funding_source_order":["wallet","subscription"]}`, "wallet_first", []string{"wallet", "subscription"}},
	} {
		var profile AccountProfile
		if err := readProfilePreference(&profile, []byte(row.settings)); err != nil {
			t.Fatal(err)
		}
		if profile.BillingPreference != row.mode || !reflect.DeepEqual(profile.FundingSourceOrder, row.order) {
			t.Fatalf("funding preference lost: %+v", profile)
		}
	}
	var profile AccountProfile
	if err := readProfilePreference(&profile, []byte(`{"funding_source_order":["subscription"],"subscription_order_ids":[9007199254740993,17]}`)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profile.SubscriptionOrderIDs, []int64{9007199254740993, 17}) {
		t.Fatal("subscription IDs were converted to account IDs or rounded")
	}
}

func TestProfileFundingPreferenceRejectsInvalidExplicitSettings(t *testing.T) {
	for _, raw := range []string{
		`null`,
		`{"billing_preference":"retired-points"}`,
		`{"funding_source_order":["wallet","wallet"]}`,
		`{"funding_source_order":["wallet","subscription","wallet"]}`,
		`{"funding_source_order":["points"]}`,
		`{"funding_source_order":1}`,
		`{"subscription_order_ids":[-1]}`,
		`{"subscription_order_ids":[0]}`,
		`{"subscription_order_ids":[17,17]}`,
		`{"subscription_order_ids":[1.5]}`,
		`{"subscription_order_ids":[9223372036854775808]}`,
	} {
		if err := readProfilePreference(&AccountProfile{}, []byte(raw)); err == nil {
			t.Fatalf("invalid explicit funding settings silently defaulted: %s", raw)
		}
	}
}

func TestProfileFundingPreferenceSurvivesImmutableWire(t *testing.T) {
	var profile AccountProfile
	if err := readProfilePreference(&profile, []byte(`{"funding_source_order":["wallet","subscription"],"subscription_order_ids":[9007199254740993,17]}`)); err != nil {
		t.Fatal(err)
	}
	cipher, err := NewAESGCM(bytes.Repeat([]byte{19}, 32))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := sealSnapshot(&Snapshot{AccountProfiles: map[int64]AccountProfile{1: profile}}, cipher)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	wire, err = decodeWireSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := openSnapshot(wire, cipher)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.AccountProfiles[1], profile) {
		t.Fatal("funding source or exact subscription ID order changed over snapshot wire")
	}
}
