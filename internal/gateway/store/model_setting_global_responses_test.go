package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProtocolBridgePolicyLegacyEnabledForcesMatchingChannels(t *testing.T) {
	policy := ChatCompletionsToResponsesPolicy{
		Enabled: false, AllChannels: true, ModelPatterns: []string{`^gpt-5`},
	}
	require.False(t, policy.IsChannelEnabled(1, 1))
	require.Equal(t, ProtocolBridgeModeAuto, policy.EffectiveMode())

	policy.Enabled = true
	policy.AllChannels = false
	policy.ChannelIDs = []int{42}
	require.Equal(t, ProtocolBridgeModeForce, policy.EffectiveMode())
	require.True(t, policy.IsChannelEnabled(42, 1))
	require.False(t, policy.IsChannelEnabled(41, 1))
}

func TestProtocolBridgePolicyExplicitModeOverridesLegacyEnabled(t *testing.T) {
	policy := ProtocolBridgePolicy{Mode: ProtocolBridgeModeDisabled, Enabled: true, AllChannels: true}
	require.Equal(t, ProtocolBridgeModeDisabled, policy.EffectiveMode())
	require.False(t, policy.IsChannelEnabled(1, 1))

	policy.Mode = "invalid"
	require.Equal(t, ProtocolBridgeModeAuto, policy.EffectiveMode())
}
