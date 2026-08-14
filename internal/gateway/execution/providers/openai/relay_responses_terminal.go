package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

func hasResponsesStreamContent(streamResponse dto.ResponsesStreamResponse) bool {
	if streamResponse.Delta != "" && strings.HasSuffix(streamResponse.Type, ".delta") {
		return true
	}
	if streamResponse.Type != dto.ResponsesOutputTypeItemAdded || streamResponse.Item == nil {
		return false
	}
	return strings.TrimSpace(streamResponse.Item.Type) != ""
}

func hasResponsesCompletedContent(streamResponse dto.ResponsesStreamResponse) bool {
	if streamResponse.Response == nil {
		return false
	}
	if len(streamResponse.Response.Output) > 0 {
		return true
	}
	usage := streamResponse.Response.Usage
	return usage != nil && (usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.TotalTokens > 0)
}

func isResponsesFailureEvent(streamResponse dto.ResponsesStreamResponse) bool {
	switch streamResponse.Type {
	case "error", "response.failed", "response.incomplete":
		return true
	default:
		return false
	}
}

func responsesFailureError(streamResponse dto.ResponsesStreamResponse) error {
	if streamResponse.Response != nil {
		if openAIError := streamResponse.Response.GetOpenAIError(); openAIError != nil && openAIError.Message != "" {
			return errors.New(openAIError.Message)
		}
	}
	if openAIError := dto.GetOpenAIError(streamResponse.Error); openAIError != nil && openAIError.Message != "" {
		return errors.New(openAIError.Message)
	}
	return fmt.Errorf("upstream returned terminal event %s", streamResponse.Type)
}

func sendSyntheticResponsesFailure(c *gin.Context, info *relaycommon.RelayInfo, message string) error {
	response := dto.ResponsesStreamResponse{
		Type: "response.failed",
		Response: &dto.OpenAIResponsesResponse{
			Status: json.RawMessage(`"failed"`),
			Error: map[string]any{
				"type":    "upstream_error",
				"code":    "incomplete_stream",
				"message": message,
			},
		},
	}
	data, err := platformencoding.Marshal(response)
	if err != nil {
		return err
	}
	if err := sendResponsesStreamData(c, info, response, string(data)); err != nil {
		return err
	}
	c.Set(string(constant.ContextKeyResponsesTerminalSent), true)
	return nil
}
