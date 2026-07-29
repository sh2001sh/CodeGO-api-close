package execution

import (
	"io"
	"testing"

	"github.com/gin-gonic/gin"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

func TestForceResponsesStreamBodyAddsStreamTrue(t *testing.T) {
	storage, err := platformhttpx.CreateBodyStorage([]byte(`{"model":"gpt-5","input":"hello"}`))
	require.NoError(t, err)
	defer storage.Close()

	body, err := forceResponsesStreamBody(storage)

	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-5","input":"hello","stream":true}`, string(body))
}

func TestForceResponsesStreamBodyOverridesStreamFalse(t *testing.T) {
	storage, err := platformhttpx.CreateBodyStorage([]byte(`{"model":"gpt-5","input":"hello","stream":false}`))
	require.NoError(t, err)
	defer storage.Close()

	body, err := forceResponsesStreamBody(storage)

	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-5","input":"hello","stream":true}`, string(body))
}

func TestBuildRemoteCompactionV2BodyPreservesProtocolFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	payload := []byte(`{"model":"gpt-5-alias","input":[{"type":"compaction_trigger"}],"stream":true,"store":true,"prompt_cache_key":"cache-1","reasoning":{"effort":"high"}}`)
	storage, err := platformhttpx.CreateBodyStorage(payload)
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(platformhttpx.KeyBodyStorage, storage)

	body, size, err := buildRemoteCompactionV2Body(ctx, "gpt-5-alias", "gpt-5-alias")
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), size)
	actual, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, payload, actual)
}

func TestBuildRemoteCompactionV2BodyMapsOnlyModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	storage, err := platformhttpx.CreateBodyStorage([]byte(`{"model":"gpt-5-alias","stream":true,"store":true,"prompt_cache_key":"cache-1"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(platformhttpx.KeyBodyStorage, storage)

	body, _, err := buildRemoteCompactionV2Body(ctx, "gpt-5-alias", "gpt-5-upstream")
	require.NoError(t, err)
	actual, err := io.ReadAll(body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-5-upstream","stream":true,"store":true,"prompt_cache_key":"cache-1"}`, string(actual))
}
