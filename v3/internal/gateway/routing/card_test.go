package routing

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestConsumptionCardsPreferEnabledChannelsAndKeepFullPriceRetry(t *testing.T) {
	s := snapshotOf("fill_first", chanSpec{id: 1, priority: 100, creds: 1, weight: 1}, chanSpec{id: 2, creds: 1, weight: 1})
	e := newEnv(s, Config{})
	s.Channels[2].MultiplierCardUserEnabled = true
	s.AccountProfiles = map[int64]catalog.AccountProfile{1: {Cards: []catalog.MultiplierCard{{ID: 1, PropType: "consume_discount_90", MultiplierPPM: 900000, ExpiresAt: e.clock.now().Add(time.Hour)}}}}
	plan := mustPlan(t, e, request(""))
	if len(plan) != 2 || plan[0].ChannelID != 2 || plan[1].ChannelID != 1 || plan[0].MultiplierPPM != 1000000 || plan[1].MultiplierPPM != 1000000 {
		t.Fatal("card preference changed base price or omitted full-price retry")
	}
	e.clock.advance(time.Hour)
	if plan := mustPlan(t, e, request("")); plan[0].ChannelID != 1 {
		t.Fatal("expired card kept routing preference")
	}
}

func TestZeroHourUsesConfiguredGroupAndRefusesExpiry(t *testing.T) {
	s := snapshotOf("fill_first", chanSpec{id: 1, creds: 1, weight: 1})
	s.Settings = map[string]json.RawMessage{"blind_box_setting.multiplier_card_route_group": json.RawMessage(`"card-group"`)}
	s.Routes["card-group"] = s.Routes["default"]
	s.Channels[1].MultiplierCardUserEnabled = true
	e := newEnv(s, Config{})
	s.AccountProfiles = map[int64]catalog.AccountProfile{1: {Cards: []catalog.MultiplierCard{{ID: 1, PropType: "zero_hour_multiplier", MultiplierPPM: 0, ExpiresAt: e.clock.now().Add(time.Second)}}}}
	r := request("")
	r.Principal.Group = "zero-hour"
	if target := mustPlan(t, e, r)[0]; target.Group != "card-group" || target.MultiplierPPM != 0 {
		t.Fatal("zero-hour token did not retain explicit route benefit")
	}
	e.clock.advance(time.Second)
	if _, err := e.planner.Plan(ctx, r); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("expired zero-hour token allowed: %v", err)
	}
	r.Principal.Group = "monthly-pass"
	if target := mustPlan(t, e, r)[0]; target.Group != "card-group" || target.MultiplierPPM != 1000000 {
		t.Fatal("monthly alias incorrectly changed global price")
	}
}
