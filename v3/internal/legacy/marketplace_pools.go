package legacy

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

func marketplaceEpoch() time.Time { return time.Unix(0, 0).UTC() }

type marketplaceTier struct {
	Name        string      `json:"name"`
	Min         json.Number `json:"min_usd"`
	Max         json.Number `json:"max_usd"`
	Probability json.Number `json:"probability"`
	Kind        string      `json:"reward_type"`
	Wallet      string      `json:"wallet_type"`
}

func marketplaceWeights(tiers []marketplaceTier) ([]int64, error) {
	denominator := big.NewInt(1)
	values := make([]*big.Rat, len(tiers))
	for i, t := range tiers {
		n, ok := new(big.Rat).SetString(t.Probability.String())
		if !ok || n.Sign() < 0 {
			return nil, fmt.Errorf("invalid reward probability")
		}
		values[i] = n
		g := new(big.Int).GCD(nil, nil, denominator, n.Denom())
		denominator.Mul(new(big.Int).Quo(denominator, g), n.Denom())
	}
	weights := make([]*big.Int, len(tiers))
	gcd := new(big.Int)
	for i, n := range values {
		weights[i] = new(big.Int).Mul(n.Num(), new(big.Int).Quo(new(big.Int).Set(denominator), n.Denom()))
		gcd.GCD(nil, nil, gcd, weights[i])
	}
	if gcd.Sign() == 0 {
		return nil, fmt.Errorf("reward pool has no positive probability")
	}
	out := make([]int64, len(tiers))
	total := new(big.Int)
	for i, n := range weights {
		n.Quo(n, gcd)
		if !n.IsInt64() {
			return nil, fmt.Errorf("reward weights overflow int64")
		}
		out[i] = n.Int64()
		total.Add(total, n)
	}
	if !total.IsInt64() {
		return nil, fmt.Errorf("reward weight sum overflows int64")
	}
	return out, nil
}

const marketplaceBalanceTiers = `[{"name":"0.20-0.80 统一额度","min_usd":0.2,"max_usd":0.8,"probability":0.52177312,"reward_type":"claude_quota"},{"name":"1.50-2.50 统一额度","min_usd":1.5,"max_usd":2.5,"probability":0.0107387,"reward_type":"claude_quota"},{"name":"2.50-3.69 统一额度","min_usd":2.5,"max_usd":3.69,"probability":0.30367707,"reward_type":"claude_quota"},{"name":"4.50-12.00 统一额度","min_usd":4.5,"max_usd":12,"probability":0.15,"reward_type":"claude_quota"},{"name":"12.00-30.00 统一额度","min_usd":12,"max_usd":30,"probability":0.0001,"reward_type":"claude_quota"},{"name":"30.00-100.00 统一额度","min_usd":30,"max_usd":100,"probability":0.00001,"reward_type":"claude_quota"},{"name":"100.00-300.00 统一额度","min_usd":100,"max_usd":300,"probability":0.000001,"reward_type":"claude_quota"},{"name":"300.00-500.00 统一额度","min_usd":300,"max_usd":500,"probability":0.0000001,"reward_type":"claude_quota"},{"name":"500.00 统一额度","min_usd":500,"max_usd":500,"probability":0.00000001,"reward_type":"claude_quota"},{"name":"再来一抽","probability":0.0127,"reward_type":"prop"},{"name":"九折充值卡","probability":0.001,"reward_type":"prop"}]`

func (d *marketplaceData) normalizePools() {
	if len(d.source["blind_box_pools"]) > 0 {
		for _, r := range d.source["blind_box_pools"] {
			d.add("blind_box_pools", r.id(), map[string]any{"price_micro": d.money(r, "price_usd", 1000000), "daily_limit": d.integer(r, "daily_limit"), "enabled": r.flag("enabled"), "name": r.text("name"), "rewards": json.RawMessage(r["rewards"]), "guarantees": json.RawMessage(r["guarantees"]), "updated_at": d.instant(r, "updated_at", true)})
		}
		return
	}
	options := make(map[string]string)
	for _, r := range d.source["options"] {
		options[r.text("key")] = r.text("value")
	}
	marketplaceEffectivePoolOptions(options)
	get := func(key, fallback string) string {
		if value, ok := options["blind_box_setting."+key]; ok {
			return value
		}
		return fallback
	}
	price, err := marketplaceExact(marketplacePositiveSetting(get("balance_blind_box_price_usd", "2.5"), "2.5"), 1000000)
	if err != nil {
		d.problem("blind_box_pool", 1, "invalid_blind_box_price", err.Error())
	}
	limit, err := strconv.ParseInt(get("balance_blind_box_daily_purchase_limit", "10"), 10, 64)
	if limit <= 0 {
		limit = 10
	}
	if err != nil || limit > 10000 {
		d.problem("blind_box_pool", 1, "invalid_blind_box_limit", "balance purchase limit outside target range")
	}
	rewards, err := d.tierRewards(get("balance_blind_box_tiers", get("tiers", marketplaceBalanceTiers)))
	if err != nil {
		d.problem("blind_box_pool", 1, "invalid_blind_box_tiers", err.Error())
	}
	guarantees := make(map[string]any)
	for _, policy := range []struct{ kind, tiers, amount, threshold string }{
		{"first", `[{"name":"首购 2.50-2.80 统一额度","min_usd":2.5,"max_usd":2.8,"probability":0.70,"reward_type":"claude_quota"},{"name":"首购 2.80-3.20 统一额度","min_usd":2.8,"max_usd":3.2,"probability":0.25,"reward_type":"claude_quota"},{"name":"首购 3.20-3.50 统一额度","min_usd":3.2,"max_usd":3.5,"probability":0.05,"reward_type":"claude_quota"}]`, "10", "0"},
		{"small", `[{"name":"小保底 2.50-3.00 统一额度","min_usd":2.5,"max_usd":3,"probability":0.65,"reward_type":"claude_quota"},{"name":"小保底 3.00-4.00 统一额度","min_usd":3,"max_usd":4,"probability":0.25,"reward_type":"claude_quota"},{"name":"小保底 4.00-6.00 统一额度","min_usd":4,"max_usd":6,"probability":0.10,"reward_type":"claude_quota"}]`, "10", "10"},
		{"big", `[{"name":"大保底 8.75-10.00 统一额度","min_usd":8.75,"max_usd":10,"probability":0.65,"reward_type":"claude_quota"},{"name":"大保底 10.00-14.00 统一额度","min_usd":10,"max_usd":14,"probability":0.25,"reward_type":"claude_quota"},{"name":"大保底 14.00-20.00 统一额度","min_usd":14,"max_usd":20,"probability":0.10,"reward_type":"claude_quota"}]`, "35", "50"},
	} {
		prefix := "balance_blind_box_" + policy.kind + "_pity_"
		if policy.kind == "big" {
			prefix = "balance_blind_box_pity_"
		}
		if policy.kind == "first" {
			prefix = "balance_blind_box_first_draw_"
		}
		rawTiers := get(prefix+"tiers", policy.tiers)
		var configured []marketplaceTier
		if json.Unmarshal([]byte(rawTiers), &configured) == nil && len(configured) == 0 {
			rawTiers = policy.tiers
		}
		rs, e := d.tierRewards(rawTiers)
		if e != nil {
			d.problem("blind_box_pool", 1, "invalid_blind_box_guarantee", e.Error())
		}
		minimum, e := marketplaceExact(marketplacePositiveSetting(get(prefix+"guarantee_usd", policy.amount), policy.amount), 250000)
		if e != nil {
			d.problem("blind_box_pool", 1, "invalid_blind_box_guarantee_amount", e.Error())
		}
		for _, r := range rs {
			if r["kind"] == "credits" {
				if n, _ := r["minimum_micro"].(int64); n < minimum {
					r["minimum_micro"] = minimum
				}
				if n, _ := r["maximum_micro"].(int64); n < minimum {
					r["maximum_micro"] = minimum
				}
			}
		}
		guarantees[policy.kind] = rs
		if policy.kind != "first" {
			n, e := strconv.ParseInt(get(prefix+"threshold", policy.threshold), 10, 64)
			if n <= 0 || n >= 1000000 {
				n, _ = strconv.ParseInt(policy.threshold, 10, 64)
			}
			if e != nil {
				d.problem("blind_box_pool", 1, "invalid_blind_box_guarantee_threshold", "threshold outside target range")
			}
			guarantees[policy.kind+"_after"] = n
			guarantees[policy.kind+"_reset_micro"] = minimum
		}
	}
	jsonRewards, _ := json.Marshal(rewards)
	jsonGuarantees, _ := json.Marshal(guarantees)
	// No source row timestamp exists for options. Unix epoch is deterministic
	// and retains idempotence rather than manufacturing a different time on replay.
	d.add("blind_box_pools", 1, map[string]any{"price_micro": price, "daily_limit": limit, "monthly_limit": int64(0), "daily_open_limit": int64(0), "enabled": get("enabled", "false") == "true" && get("balance_blind_box_enabled", "true") == "true", "name": "统一盲盒", "scope": "credits", "rewards": json.RawMessage(jsonRewards), "guarantees": json.RawMessage(jsonGuarantees), "updated_at": marketplaceEpoch()})
	d.normalizeStandardPool(options)
}

func (d *marketplaceData) normalizeStandardPool(options map[string]string) {
	get := func(key, fallback string) string {
		if v, ok := options["blind_box_setting."+key]; ok {
			return v
		}
		return fallback
	}
	// v2 Get() assigns normalized balance tiers to standard tiers and disables
	// the dormant first/pity amount options. Import the effective runtime policy.
	rewards, err := d.tierRewards(get("balance_blind_box_tiers", get("tiers", marketplaceBalanceTiers)))
	if err != nil {
		d.problem("blind_box_pool", 2, "invalid_standard_blind_box_tiers", err.Error())
	}
	price, err := marketplaceExact(marketplacePositiveSetting(get("unit_price", "2.5"), "2.5"), 1000000)
	if err != nil {
		d.problem("blind_box_pool", 2, "invalid_standard_blind_box_price", err.Error())
	}
	limit, err := strconv.ParseInt(get("daily_limit", "10"), 10, 64)
	if limit <= 0 || limit > 10 {
		limit = 10
	}
	if err != nil {
		d.problem("blind_box_pool", 2, "invalid_standard_blind_box_limit", "daily_limit outside target range")
	}
	probability, err := marketplaceExact(marketplaceProbabilitySetting(get("subscription_prize_probability", "0.003")), 1000000000)
	if err != nil || probability > 1000000000 {
		d.problem("blind_box_pool", 2, "invalid_standard_subscription_probability", "subscription probability must be exactly representable in [0,1] parts per billion")
	}
	planID := int64(0)
	planTitle := get("subscription_plan_title", "Lite月卡")
	if planTitle == "" {
		planTitle = "Lite月卡"
	}
	planTitle = strings.TrimSpace(planTitle)
	for _, r := range d.source["subscription_plans"] {
		if r.text("title") == planTitle && (planID == 0 || r.id() < planID) {
			// v2 GORM First chooses the smallest primary key for a title.
			planID = r.id()
		}
	}
	enabled := get("enabled", "false") == "true"
	if enabled && probability > 0 && planID == 0 {
		d.problem("blind_box_pool", 2, "missing_standard_subscription_plan", "conditional subscription title has no matching source plan")
	}
	standard := map[string]any{"enabled": enabled || planID > 0 || probability == 0, "subscription_probability_ppb": probability, "subscription_plan_id": planID, "first_purchase_minimum_micro": int64(0), "pity_minimum_micro": int64(0), "low_reward_threshold_micro": int64(0), "pity_after": int64(1000000)}
	monthly := d.poolLimit(get("monthly_limit", "500"), "monthly_limit", 2, 500)
	openLimit := d.poolLimit(get("daily_open_limit", "5000"), "daily_open_limit", 2, 5000)
	payload, _ := json.Marshal(rewards)
	policy, _ := json.Marshal(standard)
	d.add("blind_box_pools", 2, map[string]any{"price_micro": price, "daily_limit": limit, "monthly_limit": monthly, "daily_open_limit": openLimit, "enabled": enabled, "scope": "standard", "name": "标准盲盒", "rewards": json.RawMessage(payload), "guarantees": json.RawMessage(`{}`), "standard_policy": json.RawMessage(policy), "updated_at": marketplaceEpoch()})
}

func (d *marketplaceData) poolLimit(raw, field string, poolID, fallback int64) int64 {
	n, err := strconv.ParseInt(raw, 10, 64)
	if n <= 0 {
		n = fallback
	}
	if err != nil || n > 2147483647 {
		d.problem("blind_box_pool", poolID, "invalid_blind_box_limit", field+" must be a positive int32")
	}
	return n
}

func (d *marketplaceData) tierRewards(raw string) ([]map[string]any, error) {
	var tiers []marketplaceTier
	if err := json.Unmarshal([]byte(raw), &tiers); err != nil {
		return nil, err
	}
	weights, err := marketplaceWeights(tiers)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for i, t := range tiers {
		if weights[i] == 0 {
			continue
		}
		if t.Kind == "" && (t.Min.String() == "" || t.Min.String() == "0") && (t.Max.String() == "" || t.Max.String() == "0") {
			t.Kind = "prop"
		}
		if t.Kind == "prop" {
			switch strings.TrimSpace(t.Name) {
			case "0.10 倍率体验卡", "0.1 倍率卡", "15 分钟 0.1 倍率卡", "充值九折卡":
				t.Name = "九折充值卡"
			}
		}
		reward := map[string]any{"title": t.Name, "weight": weights[i]}
		switch t.Kind {
		case "quota", "claude_quota", "":
			minValue, e := marketplaceExact(t.Min.String(), 1000000)
			if e != nil {
				return nil, e
			}
			maxValue, e := marketplaceExact(t.Max.String(), 1000000)
			if e != nil {
				return nil, e
			}
			if minValue <= 0 || maxValue < minValue {
				return nil, fmt.Errorf("invalid reward range")
			}
			reward["kind"] = "credits"
			reward["minimum_micro"] = minValue
			reward["maximum_micro"] = maxValue
			reward["step_micro"] = int64(10000)
		case "prop":
			if named, ok := marketplaceNamedProp(t.Name); ok {
				reward = named
				reward["weight"] = weights[i]
			} else {
				return nil, fmt.Errorf("pool reward %q requires typed discount reward support", t.Name)
			}
		default:
			return nil, fmt.Errorf("unsupported pool reward type %q", t.Kind)
		}
		reward["reward_tier"] = t.Name
		if reward["kind"] == "credits" {
			// All v2 credit tiers become the unified Claude wallet at Get().
			reward["legacy_reward_type"] = "claude_quota"
			reward["wallet_type"] = "claude"
		} else {
			reward["legacy_reward_type"] = "prop"
			reward["wallet_type"] = t.Wallet
		}
		out = append(out, reward)
	}
	return out, nil
}
