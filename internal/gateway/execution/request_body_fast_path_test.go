package execution

import (
	"bytes"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

func TestTryChatCompletionsOriginalBodyFastPathReusesLargeBufferedStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	content := strings.Repeat("a", 1<<20)
	payload := []byte(fmt.Sprintf(`{"model":"gpt-4.1","messages":[{"role":"user","content":"%s"}],"stream":true}`, content))
	request := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	stream := true
	original := &dto.GeneralOpenAIRequest{Model: "gpt-4.1", Stream: &stream}
	prepared := &dto.GeneralOpenAIRequest{Model: "gpt-4.1", Stream: &stream}
	info := nativeChatFastPathInfo("gpt-4.1")

	ok, err := tryChatCompletionsOriginalBodyFastPath(context, info, original, prepared)

	require.NoError(t, err)
	require.True(t, ok)
	storage, err := platformhttpx.GetBodyStorage(context)
	require.NoError(t, err)
	body, err := storage.Bytes()
	require.NoError(t, err)
	require.Equal(t, payload, body)

	replayed, err := storage.NewReader()
	require.NoError(t, err)
	defer replayed.Close()
	replayedBody, err := io.ReadAll(replayed)
	require.NoError(t, err)
	require.Equal(t, payload, replayedBody)
}

func TestTryChatCompletionsOriginalBodyFastPathRejectsBodyRewrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	temperature := 0.7
	tests := []struct {
		name     string
		payload  string
		model    string
		original *dto.GeneralOpenAIRequest
		prepared *dto.GeneralOpenAIRequest
		mutate   func(*relaycommon.RelayInfo)
	}{
		{
			name: "gpt5 parameter normalization", model: "gpt-5.4",
			payload:  `{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"temperature":0.7}`,
			original: &dto.GeneralOpenAIRequest{Model: "gpt-5.4", Temperature: &temperature},
			prepared: &dto.GeneralOpenAIRequest{Model: "gpt-5.4", Temperature: &temperature},
		},
		{
			name: "filtered service tier", model: "gpt-4.1",
			payload:  `{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}],"service_tier":"priority"}`,
			original: &dto.GeneralOpenAIRequest{Model: "gpt-4.1"},
			prepared: &dto.GeneralOpenAIRequest{Model: "gpt-4.1"},
		},
		{
			name: "model mapping", model: "gpt-4.1",
			payload:  `{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`,
			original: &dto.GeneralOpenAIRequest{Model: "gpt-4.1"},
			prepared: &dto.GeneralOpenAIRequest{Model: "gpt-4.1-mini"},
			mutate: func(info *relaycommon.RelayInfo) {
				info.IsModelMapped = true
				info.UpstreamModelName = "gpt-4.1-mini"
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(testCase.payload))
			request.Header.Set("Content-Type", "application/json")
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = request
			info := nativeChatFastPathInfo(testCase.model)
			if testCase.mutate != nil {
				testCase.mutate(info)
			}

			ok, err := tryChatCompletionsOriginalBodyFastPath(context, info, testCase.original, testCase.prepared)

			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

func TestTryChatCompletionsOriginalBodyFastPathRejectsResolvedFileBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4.1","stream":true}`))
	context.Set(string(constant.ContextKeyResolvedFileReferences), true)
	stream := true
	request := &dto.GeneralOpenAIRequest{Model: "gpt-4.1", Stream: &stream}
	ok, err := tryChatCompletionsOriginalBodyFastPath(context, nativeChatFastPathInfo("gpt-4.1"), request, request)
	require.NoError(t, err)
	require.False(t, ok)
}

func nativeChatFastPathInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode:       gatewaycontract.RelayModeChatCompletions,
		OriginModelName: model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType:           constant.APITypeOpenAI,
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: model,
		},
	}
}
