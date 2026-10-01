package gemini

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type choiceState struct {
	started bool
	calls   int
}

type chatState struct {
	id        string
	model     string
	created   int64
	wantUsage bool
	choices   map[int]*choiceState
}

func newChatState(req *gateway.Request) *chatState {
	created := req.Received.Unix()
	if req.Received.IsZero() {
		created = time.Now().Unix()
	}
	return &chatState{id: "chatcmpl-" + req.ID, model: req.Model, created: created,
		wantUsage: gjson.GetBytes(req.Body, "stream_options.include_usage").Bool(), choices: make(map[int]*choiceState)}
}

func (s *chatState) event(response generateResponse, usage *gateway.Usage, stream bool) (gateway.Event, error) {
	if response.ID != "" {
		s.id = response.ID
	}
	if stream && len(response.Candidates) == 0 && usage != nil && !s.wantUsage {
		return gateway.Event{Kind: gateway.EventUsage, Usage: usage}, nil
	}
	if len(response.Candidates) == 0 && usage == nil {
		return gateway.Event{}, fmt.Errorf("gemini: response has neither candidates nor usage")
	}
	out := chatResponse{ID: s.id, Model: s.model, Created: s.created, Object: "chat.completion", Choices: []chatChoice{}}
	if stream {
		out.Object = "chat.completion.chunk"
	}
	for position, candidate := range response.Candidates {
		choice, err := s.candidateChoice(candidate, position, stream)
		if err != nil {
			return gateway.Event{}, err
		}
		out.Choices = append(out.Choices, choice)
	}
	if usage != nil && (!stream || s.wantUsage) {
		out.Usage = &chatUsage{Prompt: usage.PromptTokens, Completion: usage.CompletionTokens,
			Total: usage.PromptTokens + usage.CompletionTokens}
		out.Usage.Input.Cached = usage.CachedTokens
		out.Usage.Output.Reasoning = response.Usage.Thoughts
	}
	payload, err := json.Marshal(out)
	return gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: usage, TextBytes: responseTextBytes(response)}, err
}

// candidateChoice converts one Gemini candidate into a Chat choice, tracking
// per-choice state (message started, tool-call count) across chunks when
// streaming.
func (s *chatState) candidateChoice(candidate candidate, position int, stream bool) (chatChoice, error) {
	index := candidateIndex(candidate, position)
	state := s.choices[index]
	if state == nil {
		state = &choiceState{}
		s.choices[index] = state
	}
	message := &chatMessage{}
	if !stream || !state.started {
		message.Role = "assistant"
	}
	state.started = true
	for _, part := range candidate.Content.Parts {
		if err := appendCandidatePart(part, message, state, s.id, index, stream); err != nil {
			return chatChoice{}, err
		}
	}
	choice := chatChoice{Index: index}
	if candidate.FinishReason != "" {
		finish := finishReason(candidate.FinishReason, state.calls > 0)
		choice.Finish = &finish
	}
	if stream {
		choice.Delta = message
	} else {
		choice.Message = message
	}
	return choice, nil
}

// appendCandidatePart folds one generated content part (text, reasoning, or
// a function call) into message, advancing state.calls for each tool call.
func appendCandidatePart(part part, message *chatMessage, state *choiceState, id string, index int, stream bool) error {
	if part.InlineData != nil || part.FileData != nil || part.FunctionResponse != nil || len(part.ExecutableCode) > 0 || len(part.CodeResult) > 0 {
		return fmt.Errorf("gemini: unsupported generated content in chat conversion")
	}
	if part.Text == "" && part.FunctionCall == nil && part.ThoughtSignature == "" {
		return fmt.Errorf("gemini: generated part has no supported content")
	}
	if part.Thought {
		message.Reasoning += part.Text
	} else if part.Text != "" {
		if message.Content == nil {
			message.Content = new(string)
		}
		*message.Content += part.Text
	}
	if part.FunctionCall != nil {
		call := part.FunctionCall
		if len(call.Args) == 0 {
			call.Args = json.RawMessage(`{}`)
		}
		if call.Name == "" || !json.Valid(call.Args) || !gjson.ParseBytes(call.Args).IsObject() {
			return fmt.Errorf("gemini: malformed generated function call")
		}
		tool := chatToolCall{ID: fmt.Sprintf("call_%s_%d_%d", id, index, state.calls), Type: "function",
			Function: chatFunction{Name: call.Name, Arguments: string(call.Args)}}
		if stream {
			toolIndex := state.calls
			tool.Index = &toolIndex
		}
		if part.ThoughtSignature != "" {
			tool.Extra = &extraContent{Google: googleExtra{Signature: part.ThoughtSignature}}
		}
		message.Calls = append(message.Calls, tool)
		state.calls++
	} else if part.ThoughtSignature != "" {
		message.Extra = &extraContent{Google: googleExtra{Signature: part.ThoughtSignature}}
	}
	return nil
}

func finishReason(reason string, tools bool) string {
	switch reason {
	case "STOP":
		if tools {
			return "tool_calls"
		}
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
		return "content_filter"
	default:
		return "stop"
	}
}
