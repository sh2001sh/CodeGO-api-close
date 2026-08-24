package translation

import (
	"testing"

	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	"github.com/stretchr/testify/require"
)

func TestResolveProtocolBridgeModeUsesOptionalScopes(t *testing.T) {
	policy := gatewaystore.ProtocolBridgePolicy{
		Mode:        gatewaystore.ProtocolBridgeModeForce,
		AllChannels: true,
	}
	require.Equal(t, gatewaystore.ProtocolBridgeModeForce, ResolveProtocolBridgeMode(policy, 1, 1, "gpt-5"))

	policy.AllChannels = false
	policy.ChannelTypes = []int{7}
	policy.ModelPatterns = []string{`^gpt-5`}
	require.Equal(t, gatewaystore.ProtocolBridgeModeForce, ResolveProtocolBridgeMode(policy, 1, 7, "gpt-5-mini"))
	require.Equal(t, gatewaystore.ProtocolBridgeModeAuto, ResolveProtocolBridgeMode(policy, 1, 8, "gpt-5-mini"))
	require.Equal(t, gatewaystore.ProtocolBridgeModeAuto, ResolveProtocolBridgeMode(policy, 1, 7, "gpt-4o"))
}

func TestResolveProtocolBridgeModeSupportsScopedDisable(t *testing.T) {
	policy := gatewaystore.ProtocolBridgePolicy{
		Mode:       gatewaystore.ProtocolBridgeModeDisabled,
		ChannelIDs: []int{42},
	}
	require.Equal(t, gatewaystore.ProtocolBridgeModeDisabled, ResolveProtocolBridgeMode(policy, 42, 1, "any"))
	require.Equal(t, gatewaystore.ProtocolBridgeModeAuto, ResolveProtocolBridgeMode(policy, 41, 1, "any"))
}
