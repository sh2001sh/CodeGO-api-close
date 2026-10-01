package legacy

import (
	"encoding/json"
	"errors"
)

func (d *channelMarketData) prepareHistory() {
	for _, table := range []string{"verification_runs", "gpt56_mapping_runs", "ranking_snapshots", "multiplier_trend_snapshots", "pelican_artifacts"} {
		for _, r := range d.rows[table] {
			b := cmBuild()
			keys := []string{"id"}
			if table != "ranking_snapshots" {
				c, err := d.channel(r.text("channel_id"))
				if err != nil {
					b.err = err
				} else {
					b.put("channel_id", c.catalogID)
				}
			}
			if table == "ranking_snapshots" || table == "multiplier_trend_snapshots" || table == "pelican_artifacts" {
				b.texts(r, "group_id")
				if _, err := d.group(r.text("group_id")); err != nil {
					b.err = err
				}
			}
			switch table {
			case "verification_runs":
				b.texts(r, "id", "stage", "detector_name", "detector_version", "ruleset_version", "evidence_hash", "summary")
				status, err := cmVerification(r.text("status"))
				if err != nil {
					b.err = err
				}
				if status == "pending" {
					status = "queued"
				}
				b.put("status", status)
				b.put("results", json.RawMessage(`[]`))
				b.times(r, "started_at", "completed_at", "expires_at", "created_at")
			case "gpt56_mapping_runs":
				b.texts(r, "id", "parent_run_id", "level", "trigger", "status")
				b.json(r, "results", "results", "{}")
				b.times(r, "started_at", "completed_at", "created_at")
			case "ranking_snapshots":
				b.texts(r, "id", "ranking_version")
				b.integer(r, "window_hours", "window_hours")
				b.integer(r, "rank", "rank")
				b.integer(r, "latency_sample_count", "latency_sample_count")
				b.integer(r, "request_count", "request_count")
				b.integer(r, "independent_consumers", "independent_consumers")
				for _, key := range []string{"score", "raw_success_rate", "wilson_success_rate", "avg_ttft_ms", "attempt_ttft_p50_ms", "attempt_ttft_p95_ms", "e2e_ttft_p50_ms", "e2e_ttft_p95_ms", "avg_latency_ms", "avg_tps", "cache_hit_rate"} {
					if len(r[key]) > 0 {
						b.put(key, r[key])
					} else {
						b.put(key, 0)
					}
				}
				b.money(r, "avg_consumer_amount", "avg_consumer_micro")
				b.put("observing", r.text("observing") == "true")
				b.times(r, "calculated_at")
				value, err := r.structured("avg_consumer_amount_by_model", "{}")
				var amounts map[string]int64
				if err == nil {
					err = json.Unmarshal(value, &amounts)
				}
				if err != nil {
					b.err = errors.New("invalid average consumer amount by model")
				}
				for model, units := range amounts {
					micro, err := FromV2Units(units)
					if err != nil || micro < 0 {
						b.err = errors.New("invalid average consumer amount")
					} else {
						amounts[model] = int64(micro)
					}
				}
				if amounts == nil {
					amounts = map[string]int64{}
				}
				b.put("avg_consumer_micro_by_model", amounts)
			case "multiplier_trend_snapshots":
				b.integer(r, "id", "id")
				b.texts(r, "source_label")
				b.json(r, "models", "models", "[]")
				b.factor(r, "multiplier", "multiplier_ppm", false)
				b.put("reliable", r.text("reliable") == "true")
				b.integer(r, "request_count", "request_count")
				if len(r["wilson_success_rate"]) > 0 {
					b.put("wilson_success_rate", r["wilson_success_rate"])
				} else {
					b.put("wilson_success_rate", 0)
				}
				b.times(r, "bucket_started_at", "captured_at")
			case "pelican_artifacts":
				keys = []string{"group_id", "model"}
				b.texts(r, "model", "svg", "trigger", "request_id")
				actor := cmInt(r, "trigger_user_id")
				b.put("trigger_user_id", nil)
				if actor != 0 {
					b.put("trigger_user_id", actor)
					if err := d.user(actor); err != nil {
						b.err = err
					}
				}
				b.integer(r, "duration_ms", "duration_ms")
				b.times(r, "generated_at", "updated_at")
			}
			d.record("v3_channelmarket."+table, keys, b, r)
		}
	}
}
