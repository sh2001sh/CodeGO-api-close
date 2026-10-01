package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestOfficialScopeAndExactCosts(t *testing.T) {
	for _, test := range []struct {
		scope, model string
		want         bool
	}{
		{"", "anything", true}, {" gPt-4 ", "GPT-4", true}, {"*", "gpt-4", false}, {"gpt-4", "gpt-4-mini", false}, {"*", "*", true},
	} {
		if got := (OfficialPool{ModelScope: test.scope}).Matches(test.model); got != test.want {
			t.Fatalf("scope %q model %q: got %t", test.scope, test.model, got)
		}
	}
	m := OfficialPoolMember{CostMultiplier: "1.000000000000000000000001", ModelCostOverrides: map[string]json.Number{"gpt-4": "0.000000000000000000000003"}}
	if m.Cost("gpt-4") != "0.000000000000000000000003" || m.Cost("GPT-4") != m.CostMultiplier {
		t.Fatal("model cost rounding or case-insensitive override")
	}
	for _, value := range []string{"0", "-1", "NaN", "1/2", `"1.5"`, "1e1000000000", "true"} {
		if PositivePoolDecimal(value) {
			t.Fatalf("accepted invalid cost %s", value)
		}
	}
	if !PositivePoolDecimal(m.CostMultiplier) || !PositivePoolDecimal("1e-9") {
		t.Fatal("rejected valid exact decimal")
	}
}

func TestOfficialAutoDiscoveryHonorsExplicitDisableDeleteAndMembership(t *testing.T) {
	channels := map[int64]*Channel{}
	cg, cm := channelGroupSet{}, channelModelSet{}
	for id := int64(1); id <= 7; id++ {
		channels[id] = &Channel{ID: id, Scope: "official"}
		cg[id], cm[id] = map[string]bool{"default": true}, map[string]bool{"gpt-4": true}
	}
	channels[5].Scope = "marketplace"
	cg[6] = map[string]bool{"other": true}
	cm[7] = map[string]bool{"gpt-3": true}
	members := map[int64]officialMemberRow{
		1: {OfficialPoolMember: OfficialPoolMember{ChannelID: 1, CostMultiplier: "0.75"}, Enabled: true},
		2: {OfficialPoolMember: OfficialPoolMember{ChannelID: 2}, Enabled: false},
		3: {OfficialPoolMember: OfficialPoolMember{ChannelID: 3}, Enabled: true, Deleted: true},
	}
	p := OfficialPool{Group: "default", ModelScope: "GPT-4", AutoDiscover: true}
	got := compileOfficialMembers(p, members, channels, cg, cm)
	if len(got) != 2 || got[0].ChannelID != 1 || got[0].CostMultiplier != "0.75" || got[1].ChannelID != 4 || got[1].CostMultiplier != "1" || !reflect.DeepEqual(got[1].Models, []string{"gpt-4"}) {
		t.Fatalf("incorrect discovery: %+v", got)
	}
	p.AutoDiscover = false
	if got = compileOfficialMembers(p, members, channels, cg, cm); len(got) != 1 || got[0].ChannelID != 1 {
		t.Fatalf("explicit members only: %+v", got)
	}
}
