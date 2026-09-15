package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectedChannelModelNameKeepsRoutingModelSeparateFromCompactBillingModel(t *testing.T) {
	require.Equal(t, "gpt-6-astra-openai-compact", selectedChannelModelName("/v1/responses/compact", "gpt-6-astra"))
	require.Equal(t, "gpt-6-astra-openai-compact", selectedChannelModelName("/backend-api/codex/responses/compact", "gpt-6-astra"))
	require.Equal(t, "gpt-6-astra-openai-compact", selectedChannelModelName("/v1/responses/compact", "gpt-6-astra-openai-compact"))
	require.Equal(t, "gpt-6-astra", selectedChannelModelName("/v1/responses", "gpt-6-astra"))
}
