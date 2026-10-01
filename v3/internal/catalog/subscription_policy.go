package catalog

import (
	"encoding/json"
	"fmt"
	"strings"
)

type SubscriptionPolicy struct {
	Enabled       bool  `json:"enabled"`
	MultiplierPPM int64 `json:"multiplier_ppm"`
	PaidOnly      bool  `json:"paid_only"`
}

func compileSubscriptionPolicies(raw json.RawMessage) (map[string]SubscriptionPolicy, error) {
	if len(raw) == 0 {
		// These are the actual v2 runtime defaults in store/subscription_group_policy.go,
		// applied during offline compilation when no persisted option overrides them.
		return map[string]SubscriptionPolicy{
			"default": {Enabled: true, MultiplierPPM: 1000000},
			"vip":     {Enabled: true, MultiplierPPM: 1000000},
			"svip":    {Enabled: true, MultiplierPPM: 1000000},
		}, nil
	}
	return ParseSubscriptionPolicies(raw)
}

// ParseSubscriptionPolicies accepts the native object and the retained option
// API's JSON string. An absent policy remains absent rather than granting funds.
func ParseSubscriptionPolicies(raw json.RawMessage) (map[string]SubscriptionPolicy, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		raw = json.RawMessage(text)
	}
	var source map[string]struct {
		Enabled    bool        `json:"enabled"`
		Multiplier json.Number `json:"multiplier"`
		PaidOnly   bool        `json:"paid_only"`
	}
	if json.Unmarshal(raw, &source) != nil || source == nil {
		return nil, fmt.Errorf("catalog: invalid subscription group policy")
	}
	policies := make(map[string]SubscriptionPolicy, len(source))
	for group, value := range source {
		ppm, err := marketPriceMicro(value.Multiplier)
		if strings.TrimSpace(group) == "" || group != strings.TrimSpace(group) || err != nil || ppm <= 0 {
			return nil, fmt.Errorf("catalog: invalid subscription group multiplier")
		}
		policies[group] = SubscriptionPolicy{Enabled: value.Enabled, MultiplierPPM: ppm, PaidOnly: value.PaidOnly}
	}
	return policies, nil
}
