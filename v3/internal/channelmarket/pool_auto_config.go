package channelmarket

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// Client settings never control the worker's last-result or scheduling metadata.
func parseAutoBuild(raw json.RawMessage, metadata bool) (autoBuild, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return autoBuild{}, ErrInvalid
	}
	if !metadata {
		for _, key := range []string{"next_build_at", "last_build_at", "last_error"} {
			delete(object, key)
		}
		var err error
		raw, err = json.Marshal(object)
		if err != nil {
			return autoBuild{}, err
		}
	}
	var build autoBuild
	if json.Unmarshal(raw, &build) != nil {
		return build, ErrInvalid
	}
	if _, exists := object["size"]; !exists {
		build.Size = 3
	}
	if _, exists := object["interval_minutes"]; !exists {
		build.Interval = 60
	}
	if build.Schedule == "" {
		build.Schedule = "interval"
	}
	if build.Size < 1 || build.Size > 10 || build.Explore < 0 || build.Explore >= build.Size || build.Interval < 1 || build.Interval > 1440 || (build.Schedule != "interval" && build.Schedule != "daily") {
		return build, ErrInvalid
	}
	if build.Schedule == "daily" {
		if len(build.DailyTime) != 5 {
			return build, ErrInvalid
		}
		if _, err := time.Parse("15:04", build.DailyTime); err != nil {
			return build, ErrInvalid
		}
	}
	for _, weight := range []int{build.ConsumerWeight, build.SuccessWeight, build.CacheWeight, build.TTFTWeight} {
		if weight < 0 || weight > 100 {
			return build, ErrInvalid
		}
	}
	// There is no measured TTFT in the current usage records.
	build.TTFTWeight = 0
	if build.ConsumerWeight+build.SuccessWeight+build.CacheWeight == 0 {
		build.ConsumerWeight, build.SuccessWeight, build.CacheWeight = 25, 55, 20
	}
	if build.Model != "" {
		build.Models = append(build.Models, build.Model)
		build.Model = ""
	}
	if len(build.Models) > 100 {
		return build, ErrInvalid
	}
	models := make([]string, 0, len(build.Models))
	seen := map[string]bool{}
	for _, model := range build.Models {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 255 || strings.ContainsAny(model, "\x00\r\n") {
			return build, ErrInvalid
		}
		if !seen[strings.ToLower(model)] {
			models = append(models, model)
			seen[strings.ToLower(model)] = true
		}
	}
	build.Models = models
	return build, nil
}

func nextPoolBuild(build autoBuild, now time.Time) time.Time {
	now = now.UTC()
	if build.Schedule != "daily" {
		return now.Add(time.Duration(build.Interval) * time.Minute)
	}
	clock, _ := time.Parse("15:04", build.DailyTime) // validated before persistence
	next := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func preserveAutoBuildMetadata(p *RoutePool, previous json.RawMessage, now time.Time) error {
	var current, old map[string]json.RawMessage
	if json.Unmarshal(p.Config, &current) != nil {
		return ErrInvalid
	}
	if len(previous) > 0 && json.Unmarshal(previous, &old) != nil {
		return ErrInvalid
	}
	if len(current["auto_build"]) == 0 && len(old["auto_build"]) > 0 {
		current["auto_build"] = old["auto_build"]
	}
	if raw := current["auto_build"]; len(raw) > 0 {
		build, err := parseAutoBuild(raw, false)
		if err != nil {
			return err
		}
		var oldBuild autoBuild
		if len(old["auto_build"]) > 0 && json.Unmarshal(old["auto_build"], &oldBuild) != nil {
			return ErrInvalid
		}
		build.LastBuild, build.LastError = oldBuild.LastBuild, oldBuild.LastError
		if build.Enabled {
			oldSettings, err := parseAutoBuild(old["auto_build"], false)
			if err == nil && reflect.DeepEqual(buildSettings(build), buildSettings(oldSettings)) {
				build.Next = oldBuild.Next
			} else {
				next := now.UTC()
				if build.Schedule == "daily" {
					next = nextPoolBuild(build, now)
				}
				build.Next = &next
			}
		}
		current["auto_build"], err = json.Marshal(build)
		if err != nil {
			return err
		}
		p.AutoBuild = current["auto_build"]
	}
	if len(old["last_built_at"]) > 0 {
		current["last_built_at"] = old["last_built_at"]
	}
	var err error
	p.Config, err = json.Marshal(current)
	return err
}

func buildSettings(build autoBuild) autoBuild {
	build.Next, build.LastBuild, build.LastError = nil, nil, ""
	return build
}
