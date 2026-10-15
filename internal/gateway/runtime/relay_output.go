package runtime

import (
	"time"

	"github.com/sh2001sh/new-api/dto"
)

// ObserveChatStreamOutput ignores assistant roles, usage and stop-only chunks.
func (info *RelayInfo) ObserveChatStreamOutput(response dto.ChatCompletionsStreamResponse, receivedAt time.Time) {
	semantic, text := false, false
	for _, choice := range response.Choices {
		text = text || choice.Delta.GetContentString() != ""
		semantic = semantic || text || choice.Delta.GetReasoningContent() != ""
		for _, tool := range choice.Delta.ToolCalls {
			semantic = semantic || tool.ID != "" || tool.Function.Name != "" || tool.Function.Arguments != ""
		}
	}
	if semantic {
		info.ObserveStreamOutput(receivedAt, text)
	}
}

// ObserveClaudeStreamOutput excludes message setup, empty block starts and
// signatures. Thinking and tool calls count as output, separately from text.
func (info *RelayInfo) ObserveClaudeStreamOutput(response dto.ClaudeResponse, receivedAt time.Time) {
	var block *dto.ClaudeMediaMessage
	switch response.Type {
	case "content_block_start":
		block = response.ContentBlock
	case "content_block_delta":
		block = response.Delta
	}
	if block == nil {
		return
	}
	text := block.Text != nil && *block.Text != ""
	thinking := block.Thinking != nil && *block.Thinking != ""
	arguments := block.PartialJson != nil && *block.PartialJson != ""
	tool := block.Type == "tool_use" && (block.Id != "" || block.Name != "")
	if text || thinking || arguments || tool {
		info.ObserveStreamOutput(receivedAt, text)
	}
}

// ObserveResponsesStreamOutput is used by Responses-to-Chat bridges. Native
// Responses uses its own full protocol classification before forwarding events.
func (info *RelayInfo) ObserveResponsesStreamOutput(response dto.ResponsesStreamResponse, receivedAt time.Time) {
	text := response.Type == "response.output_text.delta" && response.Delta != ""
	semantic := text
	switch response.Type {
	case "response.reasoning_summary_text.delta", "response.function_call_arguments.delta":
		semantic = response.Delta != ""
	case "response.output_item.added", "response.output_item.done":
		semantic = response.Item != nil && (response.Item.Type == "function_call" || response.Item.Type == "custom_tool_call") &&
			(response.Item.Name != "" || response.Item.ArgumentsString() != "")
	}
	if semantic {
		info.ObserveStreamOutput(receivedAt, text)
	}
}
