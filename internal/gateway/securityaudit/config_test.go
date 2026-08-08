package securityaudit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigFromEnvDefaultsOff(t *testing.T) {
	t.Setenv("PROMPT_AUDIT_MODE", "")
	t.Setenv("PROMPT_AUDIT_BASE_URL", "")
	t.Setenv("PROMPT_AUDIT_ENDPOINTS_JSON", "")
	config, err := ConfigFromEnv()
	require.NoError(t, err)
	require.Equal(t, ModeOff, config.Mode)
}

func TestConfigFromEnvBuildsBlockingEndpoint(t *testing.T) {
	t.Setenv("PROMPT_AUDIT_MODE", "blocking")
	t.Setenv("PROMPT_AUDIT_BASE_URL", "https://guard.example/v1")
	t.Setenv("PROMPT_AUDIT_API_KEY", "secret")
	t.Setenv("PROMPT_AUDIT_MODEL", "guard-model")
	t.Setenv("PROMPT_AUDIT_BLOCK_CONTROVERSIAL", "true")
	t.Setenv("PROMPT_AUDIT_ENDPOINTS_JSON", "")
	config, err := ConfigFromEnv()
	require.NoError(t, err)
	require.Equal(t, ModeBlocking, config.Mode)
	require.Len(t, config.Endpoints, 1)
	require.Equal(t, "guard-model", config.Endpoints[0].Model)
	require.True(t, config.BlockControversial)
}

func TestConfigRejectsBlockingWithoutEndpoint(t *testing.T) {
	t.Setenv("PROMPT_AUDIT_MODE", "blocking")
	t.Setenv("PROMPT_AUDIT_BASE_URL", "")
	t.Setenv("PROMPT_AUDIT_ENDPOINTS_JSON", "")
	_, err := ConfigFromEnv()
	require.Error(t, err)
}
