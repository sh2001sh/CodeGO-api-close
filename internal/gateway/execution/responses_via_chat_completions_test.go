package execution

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	openaiadaptor "github.com/sh2001sh/new-api/internal/gateway/execution/providers/openai"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestResponsesViaChatCompletionsUsesChatEndpointAndRestoresResponsesShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/chat/completions", request.URL.Path)
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &upstreamBody))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"id":"chatcmpl-bridge","object":"chat.completion","created":10,"model":"gpt-test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"done","tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}
			]},"finish_reason":"tool_calls"}],
			"usage":{"prompt_tokens":6,"completion_tokens":3,"total_tokens":9}
		}`))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: json.RawMessage(`[{"type":"message","role":"user","content":"hello"}]`),
		Tools: json.RawMessage(`[{"type":"function","name":"lookup","parameters":{"type":"object"}}]`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode: gatewaycontract.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-test", RequestURLPath: "/v1/responses", Request: request,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: upstream.URL, ApiType: constant.APITypeOpenAI,
			UpstreamModelName: "gpt-test", SupportStreamOptions: true,
		},
	}
	info.InitRequestConversionChain()
	adaptor := &openaiadaptor.Adaptor{}
	adaptor.Init(info)

	usage, bridgeError := responsesViaChatCompletions(context, info, adaptor, request)
	require.Nil(t, bridgeError)
	require.Equal(t, 9, usage.TotalTokens)
	require.Equal(t, "gpt-test", upstreamBody["model"])
	require.NotEmpty(t, upstreamBody["messages"])

	var response dto.OpenAIResponsesResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "response", response.Object)
	require.Len(t, response.Output, 2)
	require.Equal(t, "message", response.Output[0].Type)
	require.Equal(t, "function_call", response.Output[1].Type)
	require.Equal(t, "call_1", response.Output[1].CallId)
	require.Equal(t, gatewaycontract.RelayModeResponses, info.RelayMode)
	require.Equal(t, "/v1/responses", info.RequestURLPath)
}
