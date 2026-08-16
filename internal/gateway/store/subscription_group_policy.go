package store

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
)

const SubscriptionGroupPolicyOptionKey = "SubscriptionGroupPolicy"

type SubscriptionGroupPolicy struct {
	Enabled    bool    `json:"enabled"`
	Multiplier float64 `json:"multiplier"`
}

var (
	subscriptionGroupPolicyMu sync.RWMutex
	subscriptionGroupPolicies = map[string]SubscriptionGroupPolicy{
		"default": {Enabled: true, Multiplier: 1},
		"vip":     {Enabled: true, Multiplier: 1},
		"svip":    {Enabled: true, Multiplier: 1},
	}
)

// GetSubscriptionGroupPolicy returns the monthly-pass policy for a billing group.
func GetSubscriptionGroupPolicy(group string) SubscriptionGroupPolicy {
	group = strings.TrimSpace(group)
	if group == "" {
		group = "default"
	}
	subscriptionGroupPolicyMu.RLock()
	policy, ok := subscriptionGroupPolicies[group]
	subscriptionGroupPolicyMu.RUnlock()
	if !ok {
		return SubscriptionGroupPolicy{Multiplier: 1}
	}
	return policy
}

func SubscriptionGroupPolicy2JSONString() string {
	subscriptionGroupPolicyMu.RLock()
	copyValue := make(map[string]SubscriptionGroupPolicy, len(subscriptionGroupPolicies))
	for group, policy := range subscriptionGroupPolicies {
		copyValue[group] = policy
	}
	subscriptionGroupPolicyMu.RUnlock()
	data, _ := json.Marshal(copyValue)
	return string(data)
}

func UpdateSubscriptionGroupPolicyByJSONString(raw string) error {
	parsed, err := parseSubscriptionGroupPolicies(raw)
	if err != nil {
		return err
	}
	subscriptionGroupPolicyMu.Lock()
	subscriptionGroupPolicies = parsed
	subscriptionGroupPolicyMu.Unlock()
	return nil
}

// ValidateSubscriptionGroupPolicyJSONString validates a policy update before it
// is persisted, keeping an invalid value out of the shared option store.
func ValidateSubscriptionGroupPolicyJSONString(raw string) error {
	_, err := parseSubscriptionGroupPolicies(raw)
	return err
}

func parseSubscriptionGroupPolicies(raw string) (map[string]SubscriptionGroupPolicy, error) {
	parsed := make(map[string]SubscriptionGroupPolicy)
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	for group, policy := range parsed {
		if strings.TrimSpace(group) == "" {
			return nil, errors.New("subscription group policy contains an empty group")
		}
		if policy.Multiplier <= 0 || math.IsNaN(policy.Multiplier) || math.IsInf(policy.Multiplier, 0) {
			return nil, errors.New("subscription group multiplier must be a positive finite number: " + group)
		}
	}
	return parsed, nil
}
