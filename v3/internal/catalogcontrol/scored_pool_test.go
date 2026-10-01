package catalogcontrol

import (
	"encoding/json"
	"testing"
)

func TestScoredPoolValidationAndNativeDefaults(t *testing.T) {
	valid := func() RoutePool {
		return RoutePool{Name: "official", Group: "default", Strategy: "scored", Enabled: true, Members: []PoolMember{{ChannelID: 1, Weight: 1, CostMultiplier: "1.12345678901234567890", ModelCostOverrides: map[string]json.Number{"gpt-4": "0.000000000000000003"}}}}
	}
	p := valid()
	if err := normalizePool(&p); err != nil || p.Model != "*" || p.ModelScope != "" || p.Members[0].Enabled == nil || !*p.Members[0].Enabled || p.Members[0].CostMultiplier != "1.12345678901234567890" {
		t.Fatalf("normalization %v %+v", err, p)
	}
	for _, mutate := range []func(*RoutePool){
		func(p *RoutePool) { p.Name = "" },
		func(p *RoutePool) { p.TTFTWeight = 101 },
		func(p *RoutePool) { p.Members[0].CostMultiplier = "0" },
		func(p *RoutePool) { p.Members[0].ModelCostOverrides["bad"] = "-1" },
		func(p *RoutePool) { p.Members = append(p.Members, p.Members[0]) },
	} {
		p = valid()
		mutate(&p)
		if normalizePool(&p) == nil {
			t.Fatalf("accepted invalid pool %+v", p)
		}
	}
	p = RoutePool{Group: "default", Model: "gpt-4", Strategy: "weighted", Members: []PoolMember{{ChannelID: 1, Weight: 1}}}
	if err := normalizePool(&p); err != nil || p.Members[0].CostMultiplier != "1" {
		t.Fatalf("native compatibility defaults: %+v %v", p, err)
	}
}
