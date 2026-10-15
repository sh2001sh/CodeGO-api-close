package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionGroupPolicyRoundTripAndDefaults(t *testing.T) {
	original := SubscriptionGroupPolicy2JSONString()
	t.Cleanup(func() { require.NoError(t, UpdateSubscriptionGroupPolicyByJSONString(original)) })

	require.NoError(t, UpdateSubscriptionGroupPolicyByJSONString(`{
		"official-gpt":{"enabled":true,"multiplier":0.8},
		"external-marketplace":{"enabled":false,"multiplier":1}
	}`))
	require.Equal(t, SubscriptionGroupPolicy{Enabled: true, Multiplier: 0.8}, GetSubscriptionGroupPolicy("official-gpt"))
	require.Equal(t, SubscriptionGroupPolicy{Multiplier: 1}, GetSubscriptionGroupPolicy("missing"))
	require.JSONEq(t, `{
		"official-gpt":{"enabled":true,"multiplier":0.8},
		"external-marketplace":{"enabled":false,"multiplier":1}
	}`, SubscriptionGroupPolicy2JSONString())
}

func TestSubscriptionGroupPolicyRejectsInvalidMultiplier(t *testing.T) {
	require.Error(t, UpdateSubscriptionGroupPolicyByJSONString(`{"official":{"enabled":true,"multiplier":0}}`))
}

func TestValidateSubscriptionGroupPolicyRejectsMalformedInput(t *testing.T) {
	require.Error(t, ValidateSubscriptionGroupPolicyJSONString(`{"official":{"enabled":true,"multiplier":0}}`))
	require.Error(t, ValidateSubscriptionGroupPolicyJSONString(`[]`))
	require.NoError(t, ValidateSubscriptionGroupPolicyJSONString(`{"official":{"enabled":false,"multiplier":1.5}}`))
}
