package catalog

import (
	"reflect"
	"sort"
	"testing"
)

func sortedRoutes(routes []Route) []Route {
	out := append([]Route(nil), routes...)
	sort.Slice(out, func(i, j int) bool { return out[i].ChannelID < out[j].ChannelID })
	return out
}

func TestBuildRoutesPlainMembership(t *testing.T) {
	channels := map[int64]*Channel{
		1: {ID: 1, Priority: 5, Weight: 0}, // weight 0 clamps to 1
		2: {ID: 2, Priority: 3, Weight: 4},
	}
	cg := channelGroupSet{1: {"default": true}, 2: {"default": true}}
	cm := channelModelSet{1: {"gpt-4": true}, 2: {"gpt-4": true}}

	routes := buildRoutes(channels, cg, cm, nil)
	got := sortedRoutes(routes["default"]["gpt-4"])
	want := []Route{
		{ChannelID: 1, Priority: 5, Weight: 1, Strategy: "weighted"},
		{ChannelID: 2, Priority: 3, Weight: 4, Strategy: "weighted"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestBuildRoutesExcludesChannelsOutsideGroupOrModel(t *testing.T) {
	channels := map[int64]*Channel{
		1: {ID: 1, Priority: 1, Weight: 1},
		2: {ID: 2, Priority: 1, Weight: 1},
	}
	cg := channelGroupSet{1: {"default": true}, 2: {"vip": true}}
	cm := channelModelSet{1: {"gpt-4": true}, 2: {"gpt-4": true}}

	routes := buildRoutes(channels, cg, cm, nil)
	got := routes["default"]["gpt-4"]
	if len(got) != 1 || got[0].ChannelID != 1 {
		t.Fatalf("expected only channel 1 in default/gpt-4, got %+v", got)
	}
	// Channel 2 belongs to vip and lists gpt-4, so it is a candidate there,
	// but channel 1 (default only) must not leak into vip/gpt-4.
	vip := routes["vip"]["gpt-4"]
	if len(vip) != 1 || vip[0].ChannelID != 2 {
		t.Fatalf("expected only channel 2 in vip/gpt-4, got %+v", vip)
	}
}

func TestBuildRoutesSpecificPoolOverridesPlainMembership(t *testing.T) {
	channels := map[int64]*Channel{
		1: {ID: 1, Priority: 1, Weight: 1},
		2: {ID: 2, Priority: 1, Weight: 1},
	}
	cg := channelGroupSet{1: {"default": true}, 2: {"default": true}}
	cm := channelModelSet{1: {"gpt-4": true}, 2: {"gpt-4": true}}
	pools := map[string]map[string]pool{
		"default": {
			"gpt-4": {strategy: "round_robin", members: []Route{
				{ChannelID: 2, Priority: 9, Weight: 1, Strategy: "round_robin"},
			}},
		},
	}

	routes := buildRoutes(channels, cg, cm, pools)
	got := routes["default"]["gpt-4"]
	want := []Route{{ChannelID: 2, Priority: 9, Weight: 1, Strategy: "round_robin"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pool should fully replace plain membership: got %+v, want %+v", got, want)
	}
}

func TestBuildRoutesWildcardPoolAppliesToEveryModel(t *testing.T) {
	channels := map[int64]*Channel{1: {ID: 1, Priority: 1, Weight: 1}}
	cg := channelGroupSet{1: {"default": true}}
	cm := channelModelSet{1: {"gpt-4": true, "claude-3": true}}
	pools := map[string]map[string]pool{
		"default": {
			"*": {strategy: "fill_first", members: []Route{
				{ChannelID: 1, Priority: 1, Weight: 1, Strategy: "fill_first"},
			}},
		},
	}

	routes := buildRoutes(channels, cg, cm, pools)
	for _, model := range []string{"gpt-4", "claude-3"} {
		got := routes["default"][model]
		want := []Route{{ChannelID: 1, Priority: 1, Weight: 1, Strategy: "fill_first"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("model %s: got %+v, want %+v", model, got, want)
		}
	}
}

func TestBuildRoutesSpecificModelPoolBeatsWildcard(t *testing.T) {
	channels := map[int64]*Channel{1: {ID: 1, Priority: 1, Weight: 1}}
	cg := channelGroupSet{1: {"default": true}}
	cm := channelModelSet{1: {"gpt-4": true}}
	pools := map[string]map[string]pool{
		"default": {
			"*": {strategy: "fill_first", members: []Route{{ChannelID: 1, Priority: 1, Weight: 1, Strategy: "fill_first"}}},
			"gpt-4": {strategy: "round_robin", members: []Route{
				{ChannelID: 1, Priority: 2, Weight: 1, Strategy: "round_robin"},
			}},
		},
	}

	routes := buildRoutes(channels, cg, cm, pools)
	got := routes["default"]["gpt-4"]
	if len(got) != 1 || got[0].Strategy != "round_robin" {
		t.Fatalf("specific pool should win over wildcard: got %+v", got)
	}
}

func TestClampWeight(t *testing.T) {
	cases := map[int]int{-5: 1, 0: 1, 1: 1, 7: 7}
	for in, want := range cases {
		if got := clampWeight(in); got != want {
			t.Errorf("clampWeight(%d) = %d, want %d", in, got, want)
		}
	}
}
