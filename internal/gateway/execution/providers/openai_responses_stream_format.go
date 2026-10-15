package providers

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	gatewaytranslation "github.com/sh2001sh/new-api/internal/gateway/translation"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"github.com/sh2001sh/new-api/types"
)

func handleNonOpenAIStreamFormat(c *gin.Context, info *relaycommon.RelayInfo, chunk *dto.ChatCompletionsStreamResponse, streamErr **types.NewAPIError) bool {
	info.SendResponseCount++

	switch info.RelayFormat {
	case types.RelayFormatClaude:
		if chunk.Usage != nil {
			info.ClaudeConvertInfo.Usage = chunk.Usage
		}
		claudeResponses := gatewaytranslation.StreamResponseOpenAI2Claude(chunk, info)
		for _, resp := range claudeResponses {
			if err := gatewaystream.ClaudeData(c, *resp); err != nil {
				*streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, 500)
				return false
			}
		}
		return true
	case types.RelayFormatGemini:
		geminiResponse := gatewaytranslation.StreamResponseOpenAI2Gemini(chunk, info)
		if geminiResponse == nil {
			return true
		}
		geminiResponseStr, err := platformencoding.Marshal(geminiResponse)
		if err != nil {
			*streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, 500)
			return false
		}
		if err := gatewaystream.StringData(c, string(geminiResponseStr)); err != nil {
			*streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, 500)
			return false
		}
		return true
	default:
		return true
	}
}

func handleNonOpenAIFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, responseID string, createAt int64, model string, usage *dto.Usage) {
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		if c != nil && c.Request != nil {
			gatewaystream.SetStreamWorkerContext(c, c.Request.Context())
		}
		claudeResponses := gatewaytranslation.FinalizeOpenAI2Claude(info, usage)
		for _, resp := range claudeResponses {
			_ = gatewaystream.ClaudeData(c, *resp)
		}
	case types.RelayFormatGemini:
		if usage == nil {
			return
		}
		finalChunk := gatewaystream.GenerateFinalUsageResponse(responseID, createAt, model, *usage)
		geminiResponse := gatewaytranslation.StreamResponseOpenAI2Gemini(finalChunk, info)
		if geminiResponse == nil {
			return
		}
		geminiResponseStr, err := platformencoding.Marshal(geminiResponse)
		if err != nil {
			return
		}
		_ = gatewaystream.StringData(c, string(geminiResponseStr))
	}
}
