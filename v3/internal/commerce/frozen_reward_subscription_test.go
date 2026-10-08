package commerce

import (
	"encoding/json"
	"errors"
	"testing"
)

func frozenTestPlan() Plan {
	return Plan{ID: 10, Name: "Fixed credits", PolicyVersion: PolicyStandardV2, Credits: 5000,
		PeriodSeconds: 86400, DurationUnit: "day", DurationValue: 1, ResetPeriod: "never"}
}

func TestFrozenRewardPlanRejectsMutableAndMalformedSpecifications(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Plan)
	}{
		{"legacy", func(p *Plan) { p.PolicyVersion = PolicyLegacy }},
		{"empty", func(p *Plan) { p.Credits = 0 }},
		{"negative", func(p *Plan) { p.Credits = -1 }},
		{"periodic", func(p *Plan) { p.PeriodCredits = 1 }},
		{"reset", func(p *Plan) { p.ResetPeriod = "daily" }},
		{"fuel", func(p *Plan) { p.FuelEnabled = true }},
		{"duration mismatch", func(p *Plan) { p.PeriodSeconds = 100 }},
		{"too short", func(p *Plan) { p.DurationUnit = "custom"; p.CustomSeconds = 59; p.PeriodSeconds = 59 }},
		{"unknown model", func(p *Plan) { p.ModelLimits = map[string]int64{"": 1} }},
		{"negative model limit", func(p *Plan) { p.ModelLimits = map[string]int64{"model": -1} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := frozenTestPlan()
			tc.edit(&p)
			body, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = decodeFrozenRewardPlan(body); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid snapshot accepted: %v", err)
			}
		})
	}
	for _, body := range []string{"", "null", "{}", `{"unexpected":true}`, `{} {}`} {
		if _, err := decodeFrozenRewardPlan(json.RawMessage(body)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("malformed snapshot %q accepted: %v", body, err)
		}
	}
}

func TestFrozenRewardPlanRetainsDisabledSpecification(t *testing.T) {
	p := frozenTestPlan()
	p.Enabled = false
	p.ModelLimits = map[string]int64{"model": 10}
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeFrozenRewardPlan(body)
	if err != nil || got.Credits != p.Credits || got.Enabled || got.ModelLimits["model"] != 10 {
		t.Fatalf("disabled frozen promise changed: %+v %v", got, err)
	}
}
