package legacy

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
)

func (d *channelMarketData) prepareChannel(c *cmChannel) {
	r, g := c.row, c.group
	b := cmBuild()
	b.put("id", g.text("id"))
	b.put("public_channel_id", c.publicID)
	b.put("channel_id", c.catalogID)
	b.put("owner_user_id", c.owner)
	b.texts(g, "public_slug", "internal_group_name", "owner_display_name", "source_type", "credit_pool_policy")
	b.put("display_name", g.text("system_display_name"))
	b.put("source_label", r.text("approved_source_label"))
	factor, factorErr := cmPublicFactor(g.text("multiplier"))
	if factorErr != nil {
		b.err = factorErr
	}
	b.put("multiplier_ppm", factor)
	b.integer(g, "routing_version", "routing_version")
	if b.values["routing_version"] == int64(0) {
		b.put("routing_version", int64(1))
	}
	lifecycle, err := cmLifecycle(g.text("lifecycle_status"))
	if err != nil {
		b.err = err
	}
	if g.text("deleted_at") != "" || r.text("deleted_at") != "" {
		lifecycle = "deleted"
	}
	verification, err := cmVerification(g.text("verification_status"))
	if err != nil {
		b.err = err
	}
	b.put("lifecycle_status", lifecycle)
	b.put("verification_status", verification)
	b.put("visibility", g.text("visibility"))
	b.put("review_reason", r.text("last_review_reason"))
	if v := g.text("visibility"); v != "public" && v != "unlisted" && v != "private" {
		b.err = errors.New("invalid group visibility")
	}
	if prices, e := cmMarketPrices(r); e != nil {
		b.err = e
	} else {
		// Import and Check both consume this exact projected record.
		b.put("model_prices", prices)
	}
	for _, key := range []string{"max_concurrency", "user_max_concurrency"} {
		value, e := r.integer(key)
		if e != nil {
			b.err = e
		} else if value < 0 || value > math.MaxInt32 {
			b.err = fmt.Errorf("invalid %s", key)
		}
	}
	b.times(g, "created_at", "updated_at", "published_at", "verification_due_at", "deleted_at")
	d.record("v3_channelmarket.groups", []string{"id"}, b, r)
	settings := map[string]any{"community": map[string]any{"id": c.publicID, "slug": g.text("public_slug"), "name": g.text("system_display_name"), "visibility": g.text("visibility"), "lifecycle_status": lifecycle, "verification_status": verification}}
	market := map[string]any{"public_channel_id": c.publicID, "group_id": g.text("id"), "multiplier_ppm": factor}
	var inactiveTimeRanges []cmRow
	for _, window := range d.rows["time_range_multipliers"] {
		if window.text("channel_id") != c.publicID {
			continue
		}
		start, startErr := window.integer("start_timestamp")
		end, endErr := window.integer("end_timestamp")
		if startErr == nil && endErr == nil && end <= start {
			inactiveTimeRanges = append(inactiveTimeRanges, window)
		}
	}
	if len(inactiveTimeRanges) > 0 {
		sort.Slice(inactiveTimeRanges, func(i, j int) bool { return inactiveTimeRanges[i].text("id") < inactiveTimeRanges[j].text("id") })
		market["legacy_inactive_time_range_multipliers"] = inactiveTimeRanges
	}
	for _, key := range []string{"submitted_source_label", "source_label_status", "source_label_review_reason", "model_consistency_status", "connectivity_test_status", "gpt56_mapping_status", "gpt56_mapping_level", "gpt56_mapping_trigger", "credential_tail", "credential_version", "max_concurrency", "user_max_concurrency", "qps", "maintenance_window", "sensitive_word_interception_enabled", "multiplier_card_supported", "multiplier_card_user_enabled", "auto_probe_enabled", "auto_probe_interval_minutes", "auto_probe_model", "auto_probe_last_status", "pelican_probe_enabled", "pelican_probe_daily_minute", "pelican_probe_model", "status"} {
		if len(r[key]) > 0 {
			market[key] = r[key]
		}
	}
	for _, key := range []string{"model_verification_results", "gpt56_mapping_results", "transport_capabilities"} {
		value, err := r.structured(key, "{}")
		if err != nil {
			d.issue("channels", r, err)
		} else {
			market[key] = value
		}
	}
	for _, key := range []string{"connectivity_test_checked_at", "gpt56_mapping_checked_at", "auto_probe_last_at", "pelican_probe_last_at"} {
		if len(r[key]) > 0 {
			market[key] = r[key]
		}
	}
	settings["market"] = market
	c.settings, _ = json.Marshal(settings)
}
