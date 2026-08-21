package execution

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestTryResponsesOriginalBodyFastPathReusesExactBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := []byte(`{"model":"gpt-5","input":[{"role":"user","content":"hello"}],"stream":true}`)
	req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req
	info := &relaycommon.RelayInfo{
		RelayMode:       gatewaycontract.RelayModeResponses,
		OriginModelName: "gpt-5",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}

	body, ok, err := tryResponsesOriginalBodyFastPath(ctx, info)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, payload, body)
}

func TestTryResponsesOriginalBodyFastPathRejectsRewriteCases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		payload string
		mutate  func(*relaycommon.RelayInfo)
	}{
		{name: "model mapping", payload: `{"model":"gpt-5","stream":true}`, mutate: func(info *relaycommon.RelayInfo) { info.IsModelMapped = true }},
		{name: "compatibility field", payload: `{"model":"gpt-5","stream":true,"include":["usage"]}`, mutate: func(info *relaycommon.RelayInfo) {}},
		{name: "disabled field", payload: `{"model":"gpt-5","stream":true,"store":true}`, mutate: func(info *relaycommon.RelayInfo) { info.ChannelOtherSettings.DisableStore = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := []byte(tt.payload)
			req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = req
			info := &relaycommon.RelayInfo{RelayMode: gatewaycontract.RelayModeResponses, OriginModelName: "gpt-5", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"}}
			tt.mutate(info)
			_, ok, err := tryResponsesOriginalBodyFastPath(ctx, info)
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

func TestForceResponsesStreamBodyAddsStreamTrue(t *testing.T) {
	storage, err := platformhttpx.CreateBodyStorage([]byte(`{"model":"gpt-5","input":"hello"}`))
	require.NoError(t, err)
	defer storage.Close()

	body, fastPath, err := forceResponsesStreamBody(storage)

	require.NoError(t, err)
	require.False(t, fastPath)
	require.JSONEq(t, `{"model":"gpt-5","input":"hello","stream":true}`, string(body))
}

func TestForceResponsesStreamBodyOverridesStreamFalse(t *testing.T) {
	storage, err := platformhttpx.CreateBodyStorage([]byte(`{"model":"gpt-5","input":"hello","stream":false}`))
	require.NoError(t, err)
	defer storage.Close()

	body, fastPath, err := forceResponsesStreamBody(storage)

	require.NoError(t, err)
	require.False(t, fastPath)
	require.JSONEq(t, `{"model":"gpt-5","input":"hello","stream":true}`, string(body))
}

func TestForceResponsesStreamBodyReusesAlreadyStreamingBody(t *testing.T) {
	storage, err := platformhttpx.CreateBodyStorage([]byte(`{"model":"gpt-5","input":"hello","stream":true}`))
	require.NoError(t, err)
	defer storage.Close()

	body, fastPath, err := forceResponsesStreamBody(storage)
	require.NoError(t, err)
	require.True(t, fastPath)
	require.Equal(t, []byte(`{"model":"gpt-5","input":"hello","stream":true}`), body)
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

func TestNormalizeRemoteCompactionV2BodyMovesTriggerToEndAndForcesStream(t *testing.T) {
	body, changed, err := normalizeRemoteCompactionV2Body([]byte(`{"model":"gpt-5","stream":false,"input":[{"type":"compaction_trigger"},{"type":"message","role":"user","content":"tail"},{"type":"compaction_trigger"}]}`), "gpt-5", "gpt-5")
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"model":"gpt-5","stream":true,"input":[{"type":"message","role":"user","content":"tail"},{"type":"compaction_trigger"}]}`, string(body))
}

func TestNormalizeRemoteCompactionV1BodyRemovesStreamOnly(t *testing.T) {
	body, changed, err := normalizeRemoteCompactionV1Body([]byte(`{"model":"gpt-5","stream":true,"input":"history","previous_response_id":"resp_1"}`))
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"model":"gpt-5","input":"history","previous_response_id":"resp_1"}`, string(body))
}

func TestNormalizePreviousResponseIDRetryRemovesStaleAnchor(t *testing.T) {
	err := types.WithOpenAIError(types.OpenAIError{Code: "previous_response_not_found", Type: "invalid_request_error", Message: "Previous response is not available"}, 400)
	retry, ok := normalizePreviousResponseIDRetry([]byte(`{"model":"gpt-5","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":"continue"}]}`), err)
	require.True(t, ok)
	require.JSONEq(t, `{"model":"gpt-5","input":[{"type":"message","role":"user","content":"continue"}]}`, string(retry))
}

func TestHasRemoteCompactionTrigger(t *testing.T) {
	require.True(t, hasRemoteCompactionTrigger(json.RawMessage(`[{"type":"message"},{"type":"compaction_trigger"}]`)))
	require.False(t, hasRemoteCompactionTrigger(json.RawMessage(`[{"type":"message"}]`)))
}

func TestNormalizedCompactionParallelToolCallsDefaultsRequiredBoolean(t *testing.T) {
	require.JSONEq(t, "false", string(normalizedCompactionParallelToolCalls(nil)))
	require.JSONEq(t, "false", string(normalizedCompactionParallelToolCalls(json.RawMessage("null"))))
	require.JSONEq(t, "true", string(normalizedCompactionParallelToolCalls(json.RawMessage("true"))))
}
