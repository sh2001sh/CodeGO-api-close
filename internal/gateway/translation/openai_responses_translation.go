package translation

import (
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/sh2001sh/new-api/dto"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

var compiledResponsesRegexCache sync.Map // map[string]*regexp.Regexp

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	text := ExtractOutputTextFromResponses(resp)

	usage := &dto.Usage{}
	if resp.Usage != nil {
		if resp.Usage.InputTokens != 0 {
			usage.PromptTokens = resp.Usage.InputTokens
			usage.InputTokens = resp.Usage.InputTokens
		}
		if resp.Usage.OutputTokens != 0 {
			usage.CompletionTokens = resp.Usage.OutputTokens
			usage.OutputTokens = resp.Usage.OutputTokens
		}
		if resp.Usage.TotalTokens != 0 {
			usage.TotalTokens = resp.Usage.TotalTokens
		} else {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
		if resp.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = resp.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CachedCreationTokens = resp.Usage.InputTokensDetails.GetCachedCreationTokens()
			usage.PromptTokensDetails.ImageTokens = resp.Usage.InputTokensDetails.ImageTokens
			usage.PromptTokensDetails.AudioTokens = resp.Usage.InputTokensDetails.AudioTokens
		}
		if resp.Usage.CompletionTokenDetails.ReasoningTokens != 0 {
			usage.CompletionTokenDetails.ReasoningTokens = resp.Usage.CompletionTokenDetails.ReasoningTokens
		}
	}

	created := resp.CreatedAt
	toolCalls := responsesOutputsToChatToolCalls(resp.Output)
	reasoning := responsesReasoningSummary(resp.Output)

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	msg := dto.Message{
		Role:    "assistant",
		Content: text,
	}
	if reasoning != "" {
		msg.ReasoningContent = &reasoning
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
	}

	out := &dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resp.Model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: *usage,
	}

	return out, usage, nil
}

func responsesOutputsToChatToolCalls(outputs []dto.ResponsesOutput) []dto.ToolCallResponse {
	var toolCalls []dto.ToolCallResponse
	for _, output := range outputs {
		if output.Type != "function_call" && output.Type != "custom_tool_call" && output.Type != "tool_search_call" {
			continue
		}
		name := strings.TrimSpace(output.Name)
		if output.Namespace != "" {
			name = qualifyResponsesToolName(output.Namespace, name)
		}
		if name == "" {
			continue
		}
		arguments := output.ArgumentsString()
		if output.Type == "custom_tool_call" {
			wrapped, _ := platformencoding.Marshal(map[string]string{"input": output.Input})
			arguments = string(wrapped)
		}
		callID := strings.TrimSpace(output.CallId)
		if callID == "" {
			callID = strings.TrimSpace(output.ID)
		}
		toolCalls = append(toolCalls, dto.ToolCallResponse{
			ID: callID, Type: "function",
			Function: dto.FunctionResponse{Name: name, Arguments: arguments},
		})
	}
	return toolCalls
}

func responsesReasoningSummary(outputs []dto.ResponsesOutput) string {
	var summary strings.Builder
	for _, output := range outputs {
		if output.Type != "reasoning" {
			continue
		}
		for _, part := range output.Summary {
			if part.Type == "summary_text" {
				summary.WriteString(part.Text)
			}
		}
		if summary.Len() == 0 {
			for _, part := range output.Content {
				if part.Text != "" {
					summary.WriteString(part.Text)
				}
			}
		}
	}
	return summary.String()
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, out := range resp.Output {
		if out.Type != "message" {
			continue
		}
		if out.Role != "" && out.Role != "assistant" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" && c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}
	for _, out := range resp.Output {
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	return sb.String()
}

func ShouldChatCompletionsUseResponsesPolicy(policy gatewaystore.ChatCompletionsToResponsesPolicy, channelID int, channelType int, model string) bool {
	return ResolveProtocolBridgeMode(policy, channelID, channelType, model) == gatewaystore.ProtocolBridgeModeForce
}

func ResolveProtocolBridgeMode(policy gatewaystore.ProtocolBridgePolicy, channelID int, channelType int, model string) gatewaystore.ProtocolBridgeMode {
	mode := policy.EffectiveMode()
	if mode == gatewaystore.ProtocolBridgeModeAuto {
		return mode
	}
	if !policy.MatchesChannel(channelID, channelType) || !matchAnyResponsesRegex(policy.ModelPatterns, model) {
		return gatewaystore.ProtocolBridgeModeAuto
	}
	return mode
}

func ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool {
	return ShouldChatCompletionsUseResponsesPolicy(
		gatewaystore.GetGlobalSettings().ChatCompletionsToResponsesPolicy,
		channelID,
		channelType,
		model,
	)
}

func ShouldResponsesUseChatCompletionsGlobal(channelID int, channelType int, model string) bool {
	return ShouldChatCompletionsUseResponsesPolicy(
		gatewaystore.GetGlobalSettings().ResponsesToChatCompletionsPolicy,
		channelID,
		channelType,
		model,
	)
}

func matchAnyResponsesRegex(patterns []string, s string) bool {
	if len(patterns) == 0 {
		return true
	}
	if s == "" {
		return false
	}
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		re, ok := compiledResponsesRegexCache.Load(pattern)
		if !ok {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			re = compiled
			compiledResponsesRegexCache.Store(pattern, re)
		}
		if re.(*regexp.Regexp).MatchString(s) {
			return true
		}
	}
	return false
}
