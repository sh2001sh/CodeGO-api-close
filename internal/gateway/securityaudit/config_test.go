package securityaudit

import (
	"testing"
	"time"

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

func TestConfigFromEnvUsesConservativePromptAuditDefaults(t *testing.T) {
	t.Setenv("PROMPT_AUDIT_MODE", "async")
	t.Setenv("PROMPT_AUDIT_BASE_URL", "https://guard.example/v1")
	t.Setenv("PROMPT_AUDIT_API_KEY", "secret")
	t.Setenv("PROMPT_AUDIT_MODEL", "")
	t.Setenv("PROMPT_AUDIT_TIMEOUT_MS", "")
	t.Setenv("PROMPT_AUDIT_INPUT_LIMIT", "")
	t.Setenv("PROMPT_AUDIT_LATEST_TURN_ONLY", "")
	t.Setenv("PROMPT_AUDIT_QUEUE_CAPACITY", "")
	t.Setenv("PROMPT_AUDIT_WORKERS", "")
	t.Setenv("PROMPT_AUDIT_GLOBAL_CONCURRENCY", "")
	t.Setenv("PROMPT_AUDIT_PER_NODE_CONCURRENCY", "")
	t.Setenv("PROMPT_AUDIT_ENDPOINTS_JSON", "")
	config, err := ConfigFromEnv()
	require.NoError(t, err)
	require.True(t, config.LatestTurnOnly)
	require.Equal(t, defaultQueueCapacity, config.QueueCapacity)
	require.Equal(t, defaultWorkerCount, config.WorkerCount)
	require.Equal(t, defaultGlobalConcurrency, config.GlobalConcurrency)
	require.Equal(t, defaultPerNodeConcurrency, config.PerNodeConcurrency)
	require.Len(t, config.Endpoints, 1)
	require.Equal(t, int(defaultTimeout/time.Millisecond), config.Endpoints[0].TimeoutMS)
	require.Equal(t, defaultInputLimit, config.Endpoints[0].InputLimit)
	require.Equal(t, DefaultModel, config.Endpoints[0].Model)
}

func TestConfigRejectsBlockingWithoutEndpoint(t *testing.T) {
	t.Setenv("PROMPT_AUDIT_MODE", "blocking")
	t.Setenv("PROMPT_AUDIT_BASE_URL", "")
	t.Setenv("PROMPT_AUDIT_ENDPOINTS_JSON", "")
	_, err := ConfigFromEnv()
	require.Error(t, err)
}
