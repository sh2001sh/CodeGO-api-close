package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type callFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}
type chunkToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function callFunction `json:"function"`
}
type chatDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   *string         `json:"content,omitempty"`
	Reasoning *string         `json:"reasoning_content,omitempty"`
	ToolCalls []chunkToolCall `json:"tool_calls,omitempty"`
}
type chunkChoice struct {
	Index        int       `json:"index"`
	Delta        chatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}
type chatChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []chunkChoice `json:"choices"`
	Usage   *chatUsage    `json:"usage,omitempty"`
}
type chatUsage struct {
	Prompt     int64 `json:"prompt_tokens"`
	Completion int64 `json:"completion_tokens"`
	Total      int64 `json:"total_tokens"`
	Details    struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func chatUsageFrom(u *gateway.Usage) *chatUsage {
	if u == nil {
		return nil
	}
	out := &chatUsage{Prompt: u.PromptTokens, Completion: u.CompletionTokens, Total: u.PromptTokens + u.CompletionTokens}
	out.Details.Cached = u.CachedTokens
	return out
}

func clientWantsUsage(body []byte) bool {
	return gjson.GetBytes(body, "stream_options.include_usage").Bool()
}

type single struct {
	body    io.ReadCloser
	chat    bool
	model   string
	created int64
	done    bool
}

func (s *single) Next() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	s.done = true
	data, err := readBody(s.body)
	if err != nil {
		return gateway.Event{}, err
	}
	var w wireMessage
	if err := json.Unmarshal(data, &w); err != nil {
		return decodeError(fmt.Errorf("anthropic: invalid response JSON: %w", err)), nil
	}
	if w.Error != nil {
		return gateway.Event{Kind: gateway.EventError, Err: upstreamError(w.Error)}, nil
	}
	if len(w.Content) == 0 {
		return gateway.Event{}, io.EOF
	}
	var state usageState
	usage, err := state.merge(w.Usage)
	if err != nil {
		return decodeError(err), nil
	}
	textBytes := 0
	for _, b := range w.Content {
		textBytes += len(b.Text) + len(b.Thinking)
		if b.Type == "tool_use" {
			textBytes += len(b.Input)
		}
	}
	if s.chat {
		data, err = convertMessage(w, s.model, s.created, usage)
		if err != nil {
			return decodeError(err), nil
		}
	}
	return gateway.Event{Kind: gateway.EventData, Payload: data, Usage: usage, TextBytes: textBytes}, nil
}

func (s *single) Close() error { return s.body.Close() }

type responseToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function callFunction `json:"function"`
}
type responseMessage struct {
	Role      string             `json:"role"`
	Content   *string            `json:"content"`
	Reasoning string             `json:"reasoning_content,omitempty"`
	ToolCalls []responseToolCall `json:"tool_calls,omitempty"`
}
type responseChoice struct {
	Index        int             `json:"index"`
	Message      responseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

func convertMessage(w wireMessage, model string, created int64, usage *gateway.Usage) ([]byte, error) {
	if w.StopReason == "" {
		return nil, fmt.Errorf("anthropic: message lacks stop_reason")
	}
	var text, thinking strings.Builder
	msg := responseMessage{Role: "assistant"}
	for _, b := range w.Content {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "thinking":
			thinking.WriteString(b.Thinking)
		case "tool_use":
			if !json.Valid(b.Input) {
				return nil, fmt.Errorf("anthropic: invalid tool response arguments")
			}
			msg.ToolCalls = append(msg.ToolCalls, responseToolCall{ID: b.ID, Type: "function", Function: callFunction{Name: b.Name, Arguments: string(b.Input)}})
		case "redacted_thinking":
		default:
			return nil, fmt.Errorf("anthropic: unsupported response block %q", b.Type)
		}
	}
	if len(w.Content) == 0 {
		return nil, fmt.Errorf("anthropic: empty message content")
	}
	if text.Len() > 0 || len(msg.ToolCalls) == 0 {
		v := text.String()
		msg.Content = &v
	}
	msg.Reasoning = thinking.String()
	out := struct {
		ID      string           `json:"id"`
		Object  string           `json:"object"`
		Created int64            `json:"created"`
		Model   string           `json:"model"`
		Choices []responseChoice `json:"choices"`
		Usage   *chatUsage       `json:"usage,omitempty"`
	}{w.ID, "chat.completion", created, model, []responseChoice{{0, msg, finishReason(w.StopReason)}}, chatUsageFrom(usage)}
	return json.Marshal(out)
}
