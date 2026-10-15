package openai

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/stretchr/testify/require"
)

func TestConvertGeminiRequestRequestsUpstreamStreamUsage(t *testing.T) {
	context, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		IsStream: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeOpenAI,
			UpstreamModelName:    "gemini-3.8-flash",
			SupportStreamOptions: true,
		},
	}
	adaptor := &Adaptor{ChannelType: constant.ChannelTypeOpenAI}

	converted, err := adaptor.ConvertGeminiRequest(context, info, &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{
			Role:  "user",
			Parts: []dto.GeminiPart{{Text: "hello"}},
		}},
	})

	require.NoError(t, err)
	request, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.NotNil(t, request.StreamOptions)
	require.True(t, request.StreamOptions.IncludeUsage)
}
