package gateway_test

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestModelDiscoveryUsesCurrentMarketplaceAndPoolPolicies(t *testing.T) {
	f := newDiscoveryFixture(t)
	snap := f.current.Load()
	snap.Channels[2].Scope = "marketplace"
	snap.Routes["market-group"] = map[string][]catalog.Route{"market-model": {{ChannelID: 2}}}
	snap.Market = catalog.MarketSnapshot{
		Groups: map[string]catalog.MarketGroupPolicy{"market-group": {
			ID: "market:2", ChannelID: 2, OwnerUserID: 8, Visibility: "private", Status: "active", Allowed: map[int64]bool{7: true},
		}},
		Channels: map[int64]catalog.MarketChannelPolicy{2: {
			GroupName: "market-group", MultiplierPPM: 2000000, Blocked: map[int64]bool{},
			ModelPrices: map[string]catalog.Price{"market-model": {Mode: "per_request", PerRequest: 10}},
		}},
	}
	f.auth.principal.Group = "market-group"
	f.auth.principal.AllowedGroups = []string{"default", "market-group", "personal-pool"}
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); !reflect.DeepEqual(ids, []string{"market-model"}) {
		t.Fatalf("market-specific priced model absent: %v", ids)
	}
	policy := snap.Market.Channels[2]
	policy.Blocked[7] = true
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); len(ids) != 0 {
		t.Fatalf("blocked user saw models: %v", ids)
	}
	delete(policy.Blocked, 7)
	f.auth.principal.MaxMarketplaceMultiplierPPM = 1000000
	if w := f.get("/v1/models/market-model", discoveryBearer); w.Code != http.StatusNotFound {
		t.Fatalf("multiplier cap bypass: %d %s", w.Code, w.Body.String())
	}
	f.auth.principal.MaxMarketplaceMultiplierPPM = 0
	group := snap.Market.Groups["market-group"]
	group.Allowed = map[int64]bool{}
	snap.Market.Groups["market-group"] = group
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); len(ids) != 0 {
		t.Fatalf("private grant revoke leaked: %v", ids)
	}
	group.Allowed[7] = true
	snap.Routes["personal-pool"] = snap.Routes["market-group"]
	members := []catalog.MarketPoolMember{{GroupID: "market:2", CatalogGroupName: "market-group"}}
	snap.Market.Pools = map[string]catalog.MarketPoolPolicy{"personal-pool": {OwnerUserID: 8, Members: members}}
	f.auth.principal.Group = "personal-pool"
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); len(ids) != 0 {
		t.Fatalf("foreign pool leaked: %v", ids)
	}
	snap.Market.Pools["personal-pool"] = catalog.MarketPoolPolicy{OwnerUserID: 7, Members: members}
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); !reflect.DeepEqual(ids, []string{"market-model"}) {
		t.Fatalf("own pool unavailable: %v", ids)
	}
	f.auth.principal.Group = "auto"
	f.auth.principal.AutoGroups = []string{"default", "market-group"}
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); !reflect.DeepEqual(ids, []string{"alpha", "market-model", "zeta"}) {
		t.Fatalf("auto union mismatch: %v", ids)
	}
}

func TestModelDiscoveryWildcardRoutesOnlyExposeKnownPricedNames(t *testing.T) {
	f := newDiscoveryFixture(t)
	snap := f.current.Load()
	snap.Routes["default"] = map[string][]catalog.Route{"*": {{ChannelID: 1}}}
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); !reflect.DeepEqual(ids, []string{"alpha", "hidden", "zeta"}) {
		t.Fatalf("wildcard known model list=%v", ids)
	}
	if w := f.get("/v1/models/invented", discoveryBearer); w.Code != http.StatusNotFound {
		t.Fatalf("wildcard invented model status=%d", w.Code)
	}
}

func TestModelDiscoveryZeroHourRequiresActiveCardAndHidesImageGeneration(t *testing.T) {
	f := newDiscoveryFixture(t)
	snap := f.current.Load()
	snap.Channels[1].MultiplierCardUserEnabled = true
	snap.Routes["纯Pro号池"] = map[string][]catalog.Route{
		"alpha": {{ChannelID: 1}}, "gpt-image-1": {{ChannelID: 1}},
	}
	snap.Prices["gpt-image-1"] = catalog.Price{Mode: "per_request", PerRequest: 10}
	f.auth.principal.Group = "zero-hour"
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); len(ids) != 0 {
		t.Fatalf("no zero-hour card leaked models: %v", ids)
	}
	snap.AccountProfiles = map[int64]catalog.AccountProfile{7: {
		Cards: []catalog.MultiplierCard{{PropType: "zero_hour_multiplier", ExpiresAt: time.Now().Add(time.Hour)}},
	}}
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); !reflect.DeepEqual(ids, []string{"alpha"}) {
		t.Fatalf("active zero-hour text models = %v", ids)
	}
	if w := f.get("/v1/models/gpt-image-1", discoveryBearer); w.Code != http.StatusNotFound {
		t.Fatalf("zero-hour image model detail: %d %s", w.Code, w.Body.String())
	}
}
