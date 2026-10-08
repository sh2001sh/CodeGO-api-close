package channelmarket

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestPoolAutoBuildRejectsNestedAndTopLevelInvalidConfig(t *testing.T) {
	invalid := []string{
		`null`, `[]`, `{"enabled":"true"}`, `{"size":0}`, `{"size":11}`,
		`{"interval_minutes":0}`, `{"interval_minutes":1441}`,
		`{"schedule":"weekly"}`, `{"schedule":"daily","daily_time":"24:00"}`,
		`{"schedule":"daily","daily_time":"3:00"}`, `{"schedule":"daily","daily_time":"03:61"}`,
		`{"explore":-1}`, `{"size":3,"explore":3}`, `{"success_weight":101}`,
		`{"consumer_weight":-1}`, `{"ttft_weight":101}`, `{"models":[""]}`,
	}
	for _, raw := range invalid {
		for _, nested := range []bool{false, true} {
			t.Run(raw+map[bool]string{true: "/nested", false: "/top"}[nested], func(t *testing.T) {
				p := RoutePool{Name: "Pool"}
				if nested {
					p.Config = json.RawMessage(`{"auto_build":` + raw + `}`)
					p.AutoBuild = json.RawMessage(`{}`) // cannot bypass invalid nested settings
				} else {
					p.AutoBuild = json.RawMessage(raw)
				}
				if _, _, err := normalizePoolInput(1, p); !errors.Is(err, ErrInvalid) {
					t.Fatalf("accepted invalid build %s: %v", raw, err)
				}
			})
		}
	}
}

func TestPoolInputCapsAndMembershipBoundaries(t *testing.T) {
	for _, value := range []json.Number{"", "0", "0.000000", "0e3", "1.250000"} {
		if _, _, err := normalizePoolInput(1, RoutePool{Name: "Pool", MaxMultiplier: value}); err != nil {
			t.Fatalf("valid multiplier %q rejected: %v", value, err)
		}
	}
	for _, value := range []json.Number{"-1", "1000001", "1/2", "garbage"} {
		if _, _, err := normalizePoolInput(1, RoutePool{Name: "Pool", MaxMultiplier: value}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid multiplier %q: %v", value, err)
		}
	}
	for _, members := range [][]PoolMember{
		{{GroupID: ""}}, {{GroupID: "g", Priority: -1}}, {{GroupID: "g", Priority: 1001}},
		{{GroupID: "g"}, {GroupID: "g"}},
	} {
		if _, _, err := normalizePoolInput(1, RoutePool{Name: "Pool", Members: members}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid members %+v: %v", members, err)
		}
	}
}

func TestPoolCanonicalEditsCannotBeOverriddenByLegacyConfig(t *testing.T) {
	legacy := json.RawMessage(`{"strategy":"cost","max_attempts":5,"failure_cooldown_seconds":30,"max_multiplier":2}`)
	p, maximum, err := normalizePoolInput(1, RoutePool{Name: "Legacy", Config: legacy})
	if err != nil || p.Strategy != "cost" || p.MaxAttempts != 5 || p.FailureCooldownSeconds != 30 || maximum != 2000000 {
		t.Fatalf("legacy input not imported %+v %v", p, err)
	}
	p, maximum, err = normalizePoolInput(1, RoutePool{Name: "Edited", Strategy: "priority", MaxAttempts: 2, FailureCooldownSeconds: 0, MaxMultiplier: "0", Config: legacy})
	if err != nil || p.Strategy != "priority" || p.MaxAttempts != 2 || p.FailureCooldownSeconds != 0 || maximum != 0 {
		t.Fatalf("stale legacy config overrode canonical edits %+v %v", p, err)
	}
}

func TestPoolAutoBuildDefaultsAndModelDeduplication(t *testing.T) {
	build, err := parseAutoBuild(json.RawMessage(`{"model":" GPT ","models":["gpt","claude"],"ttft_weight":30}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if build.Size != 3 || build.Interval != 60 || build.Schedule != "interval" || build.TTFTWeight != 0 || build.SuccessWeight != 55 || build.Model != "" || !reflect.DeepEqual(build.Models, []string{"gpt", "claude"}) {
		t.Fatalf("unexpected defaults %+v", build)
	}
	if !matchesPoolModels([]string{"Claude"}, []string{"gpt", "claude"}) || matchesPoolModels([]string{"other"}, []string{"gpt", "claude"}) {
		t.Fatal("auto pool must include any requested model and reject unrelated ones")
	}
}

func TestPoolMetadataCannotBeForgedOrResetByOwnerEdit(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	previous := json.RawMessage(`{"auto_build":{"enabled":true,"size":3,"interval_minutes":60,"last_build_at":"2026-10-06T11:00:00Z","next_build_at":"2026-10-06T13:00:00Z","last_error":"previous failure"},"last_built_at":"2026-10-06T11:00:00Z"}`)
	p, _, err := normalizePoolInput(1, RoutePool{Name: "Renamed", Config: json.RawMessage(`{"auto_build":{"enabled":true,"last_build_at":"2030-01-01T00:00:00Z","next_build_at":"2030-01-01T00:00:00Z","last_error":"forged"},"last_built_at":"forged"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = preserveAutoBuildMetadata(&p, previous, now); err != nil {
		t.Fatal(err)
	}
	build, err := parseAutoBuild(p.AutoBuild, true)
	if err != nil || build.LastBuild == nil || !build.LastBuild.Equal(now.Add(-time.Hour)) || build.Next == nil || !build.Next.Equal(now.Add(time.Hour)) || build.LastError != "previous failure" {
		t.Fatalf("forged/reset metadata %+v %v", build, err)
	}
	// Editing an unrelated manual setting also keeps the existing plan.
	p, _, err = normalizePoolInput(1, RoutePool{Name: "Manual edit"})
	if err != nil {
		t.Fatal(err)
	}
	if err = preserveAutoBuildMetadata(&p, previous, now); err != nil {
		t.Fatal(err)
	}
	build, _ = parseAutoBuild(p.AutoBuild, true)
	if !build.Enabled || build.Next == nil || !build.Next.Equal(now.Add(time.Hour)) {
		t.Fatalf("omitted automatic settings erased plan %+v", build)
	}
}

func TestPoolScheduleChangesAndDisableRetainResults(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	old := json.RawMessage(`{"auto_build":{"enabled":true,"size":3,"interval_minutes":60,"last_build_at":"2026-10-06T11:00:00Z","next_build_at":"2026-10-06T13:00:00Z","last_error":"previous failure"}}`)
	for _, enabled := range []bool{true, false} {
		raw, _ := json.Marshal(autoBuild{Enabled: enabled, Size: 3, Interval: 30})
		p, _, err := normalizePoolInput(1, RoutePool{Name: "Pool", AutoBuild: raw})
		if err != nil {
			t.Fatal(err)
		}
		if err = preserveAutoBuildMetadata(&p, old, now); err != nil {
			t.Fatal(err)
		}
		build, _ := parseAutoBuild(p.AutoBuild, true)
		if build.LastBuild == nil || build.LastError != "previous failure" {
			t.Fatalf("old result lost %+v", build)
		}
		if enabled && (build.Next == nil || !build.Next.Equal(now)) || !enabled && build.Next != nil {
			t.Fatalf("schedule change did not reschedule %+v", build)
		}
	}
}

func TestPoolDailyScheduleAlwaysUsesUTC(t *testing.T) {
	build := autoBuild{Enabled: true, Schedule: "daily", DailyTime: "03:00", Interval: 60}
	for _, sample := range []struct{ current, expected string }{
		{"2026-10-06T10:59:00+08:00", "2026-10-06T03:00:00Z"},
		{"2026-10-06T11:00:00+08:00", "2026-10-07T03:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, sample.current)
		if got := nextPoolBuild(build, now).Format(time.RFC3339); got != sample.expected {
			t.Fatalf("next %s, expected %s", got, sample.expected)
		}
	}
}

func TestPoolExploreUsesOnlyRemainingLowTrafficCandidates(t *testing.T) {
	candidates := []buildCandidate{
		{group: "best", score: 90, requests: 100},
		{group: "second", score: 80, requests: 100},
		{group: "third", score: 70, requests: 100},
		{group: "new", score: 0, requests: 0},
	}
	selected := selectBuildCandidates(candidates, autoBuild{Size: 3, Explore: 1})
	if len(selected) != 3 || selected[0].group != "best" || selected[1].group != "second" || selected[2].group != "new" {
		t.Fatalf("selection %+v", selected)
	}
}
