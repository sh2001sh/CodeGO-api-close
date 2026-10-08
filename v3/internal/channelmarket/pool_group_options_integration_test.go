//go:build pgintegration

package channelmarket_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestRoutePoolGroupOptionsIgnoreEnrichedMarketMetadata(t *testing.T) {
	f := setup(t)
	public := f.channel(t, "public")
	f.active(t, public)
	newer := f.channel(t, "public")
	f.active(t, newer)
	if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET created_at=CASE WHEN id=$1 THEN '2026-09-30 12:00:00+00'::timestamptz ELSE '2026-09-30 13:00:00+00'::timestamptz END WHERE id=ANY($2::text[])`, public.GroupID, []string{public.GroupID, newer.GroupID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market,auto_probe_enabled}','"invalid-bool"'::jsonb) WHERE id=$1`, public.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	// A corrupt optional field demonstrably breaks the enriched browsing DTO,
	// but routing only needs its stable identity, name, factor and models.
	if _, err := f.s.List(ctx, channelmarket.Actor{UserID: 2}, false); err == nil {
		t.Fatal("fixture must break unrelated enriched market metadata")
	}
	mux := http.NewServeMux()
	f.s.Register(mux, func(*http.Request) (channelmarket.Actor, error) { return channelmarket.Actor{UserID: 2}, nil })
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/marketplace/route-pools/group-options", nil))
	if w.Code != 200 {
		t.Fatalf("unrelated market metadata broke routing candidates: %d %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data []struct {
			GroupID      string      `json:"group_id"`
			DisplayID    string      `json:"display_id"`
			RoutingGroup string      `json:"routing_group"`
			Name         string      `json:"name"`
			Multiplier   json.Number `json:"multiplier"`
			Models       []string    `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 2 || envelope.Data[0].GroupID != newer.GroupID {
		t.Fatalf("unexpected candidate count: %+v", envelope.Data)
	}
	option := envelope.Data[1]
	if option.GroupID != public.GroupID || option.DisplayID != public.ID || option.RoutingGroup != public.RoutingGroup || option.Name != public.Name || option.Multiplier != public.Multiplier || len(option.Models) != 1 || option.Models[0] != "fixture-model" {
		t.Fatalf("lightweight projection changed routing identity or labels: %+v", option)
	}
}

func TestRoutePoolGroupOptionsExcludesUnroutableMarketGroups(t *testing.T) {
	f := setup(t)
	usable := f.channel(t, "public")
	f.active(t, usable)
	noModels := f.channel(t, "public")
	f.active(t, noModels)
	noCredentials := f.channel(t, "public")
	f.active(t, noCredentials)
	if _, err := f.pool.Exec(ctx, `DELETE FROM v3_catalog.channel_models WHERE channel_id=$1`, noModels.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_catalog.channel_credentials SET status='disabled' WHERE channel_id=$1`, noCredentials.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	for _, group := range []channelmarket.ChannelView{noModels, noCredentials} {
		if _, err := f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Unroutable candidate " + group.ID, Members: []channelmarket.PoolMember{{GroupID: group.GroupID}}}); !errors.Is(err, channelmarket.ErrNotFound) {
			t.Fatalf("fixture should be rejected by SavePool: %s %v", group.ID, err)
		}
	}
	market, err := f.s.List(ctx, channelmarket.Actor{UserID: 2}, false)
	if err != nil || len(market) != 3 {
		t.Fatalf("ordinary market contract unexpectedly filtered service metadata: %+v %v", market, err)
	}
	mux := http.NewServeMux()
	f.s.Register(mux, func(*http.Request) (channelmarket.Actor, error) { return channelmarket.Actor{UserID: 2}, nil })
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/marketplace/route-pools/group-options", nil))
	if w.Code != 200 {
		t.Fatalf("candidate fetch failed: %d %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data []struct {
			GroupID string `json:"group_id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 || envelope.Data[0].GroupID != usable.GroupID {
		t.Fatalf("candidates disagree with SavePool for missing models/credentials: %+v", envelope.Data)
	}
	if _, err = f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Usable candidate", Members: []channelmarket.PoolMember{{GroupID: usable.GroupID}}}); err != nil {
		t.Fatalf("listed usable candidate not saveable: %v", err)
	}
}

func TestRoutePoolGroupOptionsIncludeUsableOfficialAndAccessibleMarketGroups(t *testing.T) {
	f := setup(t)
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name,description,multiplier) VALUES('default','Official models',0.5),('disabled','Disabled',1),('no-key','Missing credentials',1),('disabled-key','Inactive credentials',1),('internal-only','Not permitted',1),('empty','No services',1);
	INSERT INTO v3_platform.settings(key,value) VALUES('UserUsableGroups','{"default":"Official","disabled":"Disabled","no-key":"No key","disabled-key":"Disabled key","empty":"Empty"}');
	INSERT INTO v3_catalog.channels(id,name,provider,status,scope) VALUES(9001,'Official usable','openai','enabled','official'),(9002,'Official disabled','openai','disabled','official'),(9003,'Official no-key','openai','enabled','official'),(9004,'Official disabled-key','openai','enabled','official'),(9005,'Official internal','openai','enabled','official');
	INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(9001,'default'),(9002,'disabled'),(9003,'no-key'),(9004,'disabled-key'),(9005,'internal-only');
	INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(9001,'official-model'),(9002,'disabled-model'),(9003,'nokey-model'),(9004,'disabledkey-model'),(9005,'internal-model');
	INSERT INTO v3_catalog.channel_credentials(channel_id,secret,status) VALUES(9001,'fixture','enabled'),(9002,'fixture','enabled'),(9004,'fixture','disabled'),(9005,'fixture','enabled')`); err != nil {
		t.Fatal(err)
	}
	public := f.channel(t, "public")
	f.active(t, public)
	private := f.channel(t, "private")
	f.active(t, private)
	shared := f.channel(t, "private")
	f.active(t, shared)
	blocked := f.channel(t, "public")
	f.active(t, blocked)
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.group_access(group_id,user_id) VALUES($1,2)`, shared.GroupID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetBlock(ctx, channelmarket.Actor{UserID: 1}, blocked.InternalChannelID, 2, true); err != nil {
		t.Fatal(err)
	}
	pool, err := f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Personal pool", Members: []channelmarket.PoolMember{{GroupID: "official:default"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(9001,$1)`, pool.TokenGroup); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	f.s.Register(mux, func(r *http.Request) (channelmarket.Actor, error) { return channelmarket.Actor{UserID: 2}, nil })
	// Existing Key dropdown contains only marketplace entries; the pool editor
	// needs its own combined projection without changing key binding semantics.
	legacy := httptest.NewRecorder()
	mux.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/api/marketplace/key-group-options", nil))
	if legacy.Code != 200 || strings.Contains(legacy.Body.String(), "official:default") {
		t.Fatalf("legacy key options contract changed: %d %s", legacy.Code, legacy.Body.String())
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/marketplace/route-pools/group-options", nil))
	if w.Code != 200 {
		t.Fatalf("combined route pool candidates unavailable: %d %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data []struct {
			GroupID      string      `json:"group_id"`
			RoutingGroup string      `json:"routing_group"`
			Name         string      `json:"name"`
			DisplayID    string      `json:"display_id"`
			Kind         string      `json:"kind"`
			Multiplier   json.Number `json:"multiplier"`
			Models       []string    `json:"models"`
		} `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 3 {
		t.Fatalf("expected one official plus public and invited market, got %+v", envelope.Data)
	}
	byID := map[string]bool{}
	for _, option := range envelope.Data {
		byID[option.GroupID] = true
		if option.Models == nil {
			t.Fatalf("nil model array: %+v", option)
		}
		if option.GroupID == "official:default" && (option.RoutingGroup != "default" || option.Kind != "official" || option.Name != "Official models" || option.DisplayID != "default" || option.Multiplier != "0.500000" || len(option.Models) != 1 || option.Models[0] != "official-model") {
			t.Fatalf("incorrect official DTO: %+v", option)
		}
		if option.GroupID == public.GroupID && (option.RoutingGroup != public.RoutingGroup || option.Kind != "market" || option.DisplayID != public.ID || option.Name != public.Name) {
			t.Fatalf("incorrect market DTO: %+v", option)
		}
	}
	for _, id := range []string{"official:default", public.GroupID, shared.GroupID} {
		if !byID[id] {
			t.Fatalf("permitted candidate absent: %s", id)
		}
	}
	for _, id := range []string{private.GroupID, blocked.GroupID, "official:disabled", "official:no-key", "official:disabled-key", "official:internal-only", "official:empty", "official:" + pool.TokenGroup} {
		if byID[id] {
			t.Fatalf("ineligible candidate leaked: %s", id)
		}
	}
	ownerMux := http.NewServeMux()
	f.s.Register(ownerMux, func(r *http.Request) (channelmarket.Actor, error) { return channelmarket.Actor{UserID: 1}, nil })
	owner := httptest.NewRecorder()
	ownerMux.ServeHTTP(owner, httptest.NewRequest(http.MethodGet, "/api/marketplace/route-pools/group-options", nil))
	if owner.Code != 200 || !strings.Contains(owner.Body.String(), private.GroupID) {
		t.Fatalf("owner private candidate absent: %d %s", owner.Code, owner.Body.String())
	}
	if _, err = f.pool.Exec(ctx, `UPDATE v3_catalog.channels SET status='disabled'`); err != nil {
		t.Fatal(err)
	}
	empty := httptest.NewRecorder()
	mux.ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/api/marketplace/route-pools/group-options", nil))
	if empty.Code != 200 || !strings.Contains(empty.Body.String(), `"data":[]`) {
		t.Fatalf("empty candidates must remain a JSON array: %d %s", empty.Code, empty.Body.String())
	}
}
