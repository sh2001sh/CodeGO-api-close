package translation

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sh2001sh/new-api/dto"
)

var ErrIncompleteToolCallContext = errors.New("tool call context is incomplete")

// validateChatToolCallHistory ensures Chat Completions tool messages can be
// represented as a self-contained Responses input sequence.
func validateChatToolCallHistory(messages []dto.Message) error {
	pending := make(map[string]struct{})
	seen := make(map[string]struct{})

	for index, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role == "tool" || role == "function" {
			if err := resolveToolCallOutput(pending, message.ToolCallId, index); err != nil {
				return err
			}
			continue
		}

		if len(pending) > 0 {
			return missingToolCallOutputsError(pending, index)
		}
		if role != "assistant" {
			continue
		}
		if err := registerAssistantToolCalls(pending, seen, message, index); err != nil {
			return err
		}
	}

	if len(pending) > 0 {
		return missingToolCallOutputsError(pending, len(messages))
	}
	return nil
}

func registerAssistantToolCalls(pending, seen map[string]struct{}, message dto.Message, index int) error {
	if len(message.ToolCalls) == 0 {
		return nil
	}
	var calls []dto.ToolCallRequest
	if err := json.Unmarshal(message.ToolCalls, &calls); err != nil {
		return fmt.Errorf("%w: assistant message %d has invalid tool_calls", ErrIncompleteToolCallContext, index)
	}

	for _, call := range calls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			return fmt.Errorf("%w: assistant message %d has a tool call without an id", ErrIncompleteToolCallContext, index)
		}
		if call.Type != "" && call.Type != "function" {
			return fmt.Errorf("%w: assistant message %d uses unsupported tool type %q", ErrIncompleteToolCallContext, index, call.Type)
		}
		if strings.TrimSpace(call.Function.Name) == "" {
			return fmt.Errorf("%w: assistant message %d has a function call without a name", ErrIncompleteToolCallContext, index)
		}
		if _, exists := seen[callID]; exists {
			return fmt.Errorf("%w: duplicate tool call id %q", ErrIncompleteToolCallContext, callID)
		}
		seen[callID] = struct{}{}
		pending[callID] = struct{}{}
	}
	return nil
}

func resolveToolCallOutput(pending map[string]struct{}, callID string, index int) error {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return fmt.Errorf("%w: tool output at message %d is missing tool_call_id", ErrIncompleteToolCallContext, index)
	}
	if _, found := pending[callID]; !found {
		return fmt.Errorf("%w: tool output at message %d does not match a pending function call %q", ErrIncompleteToolCallContext, index, callID)
	}
	delete(pending, callID)
	return nil
}

func missingToolCallOutputsError(pending map[string]struct{}, index int) error {
	callIDs := make([]string, 0, len(pending))
	for callID := range pending {
		callIDs = append(callIDs, callID)
	}
	sort.Strings(callIDs)
	return fmt.Errorf("%w: tool output is missing before message %d for function call(s) %s", ErrIncompleteToolCallContext, index, strings.Join(callIDs, ", "))
}
