package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestAIHubFeedbackWritesAnonymousMappedEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "feedback", "events.jsonl")
	t.Setenv(aiHubFeedbackPathEnv, path)
	t.Setenv(aiHubChannelMapEnv, `{"50":11016}`)

	context := &gin.Context{}
	context.Set("channel_fault_domain", "aihub:63")
	context.Set(constant.RequestIdKey, "request-secret-must-not-be-written")
	RecordAIHubHealthFailure(context, 50, "gpt-5.6-luna", 503, "model_unavailable")

	var content []byte
	require.Eventually(t, func() bool {
		var err error
		content, err = os.ReadFile(path)
		return err == nil && len(content) > 0
	}, time.Second, 10*time.Millisecond)

	var event codeGoHealthEvent
	require.NoError(t, json.Unmarshal(content, &event))
	require.Equal(t, 50, event.ChannelID)
	require.Equal(t, int64(11016), event.KeyID)
	require.Equal(t, int64(63), event.GroupID)
	require.Equal(t, "gpt-5.6-luna", event.Model)
	require.Equal(t, 503, event.StatusCode)
	require.NotEmpty(t, event.RequestIDHash)
	require.NotContains(t, string(content), "request-secret-must-not-be-written")
}

func TestAIHubFeedbackIgnoresNonTransientFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv(aiHubFeedbackPathEnv, path)
	RecordAIHubHealthFailure(&gin.Context{}, 50, "gpt-5.6-luna", 401, "unknown")

	time.Sleep(25 * time.Millisecond)
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}
