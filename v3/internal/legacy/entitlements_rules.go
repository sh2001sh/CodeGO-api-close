package legacy

import (
	"fmt"
	"math/big"
	"regexp"
	"time"
)

var luckyDigits = regexp.MustCompile(`^[0-9]{4}$`)

func entitlementRules(table string, v map[string]any) error {
	for _, field := range []string{"user_id", "inviter_id", "invitee_id", "draw_id", "reward_id", "plan_id"} {
		if n, exists := v[field].(int64); exists && n <= 0 {
			return fmt.Errorf("%s must be positive", field)
		}
	}
	for _, field := range []string{"full_match_count", "earned_total", "used_total", "available_total", "balance_after", "expected_count", "granted_count", "base_price_cents"} {
		if n, exists := v[field].(int64); exists && n < 0 {
			return fmt.Errorf("%s must be nonnegative", field)
		}
	}
	for _, field := range []string{"request_id", "card_code", "idempotency_key", "event_key", "version", "group_name", "benefit_cycle", "rule_version"} {
		if value, exists := v[field].(string); exists && value == "" {
			return fmt.Errorf("%s must not be empty", field)
		}
	}
	for _, field := range []string{"draw_date", "last_used_month", "used_month"} {
		if date, exists := v[field].(string); exists && (field == "draw_date" || date != "") {
			layout := "2006-01-02"
			if field != "draw_date" {
				layout = "2006-01"
			}
			if _, err := time.Parse(layout, date); err != nil {
				return fmt.Errorf("%s has invalid calendar format", field)
			}
		}
	}
	for _, field := range []string{"lucky_suffix", "lucky_number", "winning_number"} {
		if value, exists := v[field].(string); exists && !luckyDigits.MatchString(value) {
			return fmt.Errorf("%s must contain exactly four digits", field)
		}
	}
	if tier, exists := v["membership_tier"]; exists {
		switch tier {
		case "none", "lite", "standard", "pro", "ultra":
		default:
			return fmt.Errorf("unknown membership tier")
		}
	}
	switch table {
	case "subscription_lucky_draws":
		if v["status"] != "completed" {
			return fmt.Errorf("nonterminal lucky draw; stop scheduling and drain v2 settlement before import")
		}
		if v["draw_hour"].(int64) < 0 || v["draw_hour"].(int64) > 23 || v["draw_minute"].(int64) < 0 || v["draw_minute"].(int64) > 59 {
			return fmt.Errorf("draw clock outside valid range")
		}
		if _, err := time.LoadLocation(v["timezone"].(string)); err != nil {
			return fmt.Errorf("invalid draw timezone")
		}
	case "subscription_lucky_rewards":
		if v["credit_status"] != "credited" {
			return fmt.Errorf("uncredited lucky reward; drain v2 settlement before import")
		}
		if v["matched_digits"].(int64) < 0 || v["matched_digits"].(int64) > 4 {
			return fmt.Errorf("matched digits outside 0..4")
		}
		switch v["participation_type"] {
		case "subscription":
			if v["subscription_id"] == nil || v["open_record_id"] != nil {
				return fmt.Errorf("subscription reward requires only subscription reference")
			}
		case "blind_box":
			if v["subscription_id"] != nil || v["open_record_id"] == nil {
				return fmt.Errorf("blind-box reward requires only open-record reference")
			}
		default:
			return fmt.Errorf("unknown lucky reward participation")
		}
	case "subscription_blind_box_benefit_cycles":
		if v["status"] != "completed" || v["expected_count"] != v["granted_count"] {
			return fmt.Errorf("incomplete subscription box benefit; drain v2 grants before import")
		}
		if !v["ends_at"].(time.Time).After(v["starts_at"].(time.Time)) {
			return fmt.Errorf("benefit end must follow start")
		}
	case "subscription_reset_opportunity_accounts":
		if v["used_total"].(int64) > v["earned_total"].(int64) || v["available_total"].(int64) != v["earned_total"].(int64)-v["used_total"].(int64) {
			return fmt.Errorf("reset earned-used must equal available")
		}
	case "subscription_reset_opportunity_ledgers":
		validEarn := v["change_type"] == "earn" && v["delta"] == int64(1)
		validUse := v["change_type"] == "use" && v["delta"] == int64(-1)
		if !validEarn && !validUse {
			return fmt.Errorf("reset ledger must earn +1 or use -1")
		}
		if v["change_type"] == "use" && (v["used_month"] == "" || v["source_type"] != "user_subscription" || v["related_subscription_id"] == nil) {
			return fmt.Errorf("reset use requires month and source subscription")
		}
	case "referral_purchase_rewards":
		if v["inviter_id"] == v["invitee_id"] {
			return fmt.Errorf("referral inviter and invitee must differ")
		}
	case "subscription_claude_conversions":
		if v["status"] != "completed" || v["conversion_percent"].(int64) < 0 || v["conversion_percent"].(int64) > 100 || v["ratio_denominator"].(int64) <= 0 || v["ratio_numerator"].(int64) < 0 {
			return fmt.Errorf("invalid completed subscription conversion")
		}
		ratio, _ := new(big.Rat).SetString(v["unused_ratio"].(string))
		if ratio.Cmp(big.NewRat(1, 1)) > 0 {
			return fmt.Errorf("unused ratio exceeds one")
		}
	}
	return nil
}
