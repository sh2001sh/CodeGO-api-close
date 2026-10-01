package ollama

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type responseStream struct {
	body      io.ReadCloser
	scanner   *bufio.Scanner
	model     string
	id        string
	created   int64
	wantUsage bool
	stream    bool
	finished  bool
	delivered bool
	toolCount int
	queue     []gateway.Event
}

func (s *responseStream) init() {
	if s.stream {
		s.scanner = bufio.NewScanner(s.body)
		s.scanner.Buffer(make([]byte, 4096), maxJSONBody)
	}
}

func (s *responseStream) Close() error { return s.body.Close() }

func (s *responseStream) Next() (gateway.Event, error) {
	for {
		if len(s.queue) > 0 {
			ev := s.queue[0]
			s.queue = s.queue[1:]
			return ev, nil
		}
		if s.finished {
			return gateway.Event{}, io.EOF
		}
		data, err := s.read()
		if err != nil {
			s.finished = true
			return gateway.Event{}, err
		}
		if len(data) == 0 {
			continue
		}
		in, event, ok := s.decode(data)
		if !ok {
			return event, nil
		}
		if !s.stream {
			s.finished = true
			return s.blockingResult(in)
		}
		event, err, cont := s.handleStreamed(in)
		if cont {
			continue
		}
		return event, err
	}
}

// decode parses and validates one native response envelope. ok is false
// when Next should return (event, nil) immediately because the envelope is
// malformed or reports an upstream error.
func (s *responseStream) decode(data []byte) (nativeResponse, gateway.Event, bool) {
	var in nativeResponse
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() || json.Unmarshal(data, &in) != nil {
		s.finished = true
		return in, invalidResponse("invalid Ollama JSON response"), false
	}
	if in.Error != "" {
		s.finished = true
		return in, gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{
			Status: http.StatusBadGateway, Type: "upstream_error", Code: "ollama_error", Message: in.Error,
		}}, false
	}
	if !gjson.GetBytes(data, "message").IsObject() {
		s.finished = true
		return in, invalidResponse("Ollama response has no message"), false
	}
	if (in.PromptEvalCount != nil && *in.PromptEvalCount < 0) || (in.EvalCount != nil && *in.EvalCount < 0) {
		s.finished = true
		return in, invalidResponse("Ollama returned negative token counts"), false
	}
	if in.Message.Role != "" && in.Message.Role != "assistant" {
		s.finished = true
		return in, invalidResponse("Ollama response role must be assistant"), false
	}
	for _, call := range in.Message.ToolCalls {
		if call.Function.Name == "" || !gjson.ParseBytes(call.Function.Arguments).IsObject() {
			s.finished = true
			return in, invalidResponse("Ollama tool call requires a named function and object arguments"), false
		}
	}
	if s.created <= 0 {
		if timestamp, err := time.Parse(time.RFC3339Nano, in.CreatedAt); err == nil {
			s.created = timestamp.Unix()
		}
	}
	return in, gateway.Event{}, true
}

func (s *responseStream) blockingResult(in nativeResponse) (gateway.Event, error) {
	if !in.Done {
		return invalidResponse("Ollama non-streaming response is incomplete"), nil
	}
	if !hasOutput(in.Message) {
		return emptyResponse(), nil
	}
	return s.single(in)
}

// handleStreamed emits the Chat chunk for one streamed native response and
// queues the trailing usage/done events once Ollama reports done. cont is
// true when Next's loop should read the next line without returning (a
// lifecycle-only prefix with no output yet).
func (s *responseStream) handleStreamed(in nativeResponse) (gateway.Event, error, bool) {
	if in.Done {
		s.finished = true
		if !hasOutput(in.Message) && !s.delivered {
			return emptyResponse(), nil, false
		}
	}
	if !in.Done && !hasOutput(in.Message) {
		return gateway.Event{}, nil, true // A lifecycle-only prefix must not commit an attempt.
	}
	ev, err := s.chunk(in)
	if err != nil {
		s.finished = true
		return gateway.Event{}, err, false
	}
	s.delivered = true
	if in.Done {
		if err := s.queueTrailingEvents(in); err != nil {
			return gateway.Event{}, err, false
		}
	}
	return ev, nil, false
}

func (s *responseStream) queueTrailingEvents(in nativeResponse) error {
	if usage := usageOf(in); usage != nil {
		usageEvent := gateway.Event{Kind: gateway.EventUsage, Usage: usage}
		if s.wantUsage {
			usageEvent.Kind = gateway.EventData
			var err error
			usageEvent.Payload, err = json.Marshal(chatChunk{ID: s.id, Object: "chat.completion.chunk", Created: s.created,
				Model: s.model, Choices: []chunkChoice{}, Usage: usageJSON(usage)})
			if err != nil {
				return err
			}
		}
		s.queue = append(s.queue, usageEvent)
	}
	s.queue = append(s.queue, gateway.Event{Kind: gateway.EventDone})
	return nil
}

func (s *responseStream) read() ([]byte, error) {
	if s.stream {
		if s.scanner.Scan() {
			return s.scanner.Bytes(), nil
		}
		if err := s.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.ErrUnexpectedEOF // Native streams require done:true.
	}
	data, err := io.ReadAll(io.LimitReader(s.body, maxJSONBody+1))
	if err == nil && len(data) > maxJSONBody {
		err = fmt.Errorf("ollama: response exceeds %d bytes", maxJSONBody)
	}
	if err == nil && len(data) == 0 {
		err = io.ErrUnexpectedEOF
	}
	return data, err
}

func (s *responseStream) chunk(in nativeResponse) (gateway.Event, error) {
	delta := chatDelta{Content: in.Message.Content, Reasoning: in.Message.Thinking}
	if !s.delivered {
		delta.Role = "assistant"
	}
	if len(in.Message.ToolCalls) > 0 {
		delta.ToolCalls = s.toolCalls(in.Message.ToolCalls, true)
	}
	var finish *string
	if in.Done {
		reason := finishReason(in.DoneReason, s.toolCount > 0)
		finish = &reason
	}
	data, err := json.Marshal(chatChunk{ID: s.id, Object: "chat.completion.chunk", Created: s.created, Model: s.model,
		Choices: []chunkChoice{{Index: 0, Delta: delta, FinishReason: finish}}})
	return gateway.Event{Kind: gateway.EventData, Payload: data,
		TextBytes: outputBytes(in.Message)}, err
}

func (s *responseStream) single(in nativeResponse) (gateway.Event, error) {
	message := chatOutputMessage{Role: "assistant", Content: in.Message.Content, Reasoning: in.Message.Thinking}
	if len(in.Message.ToolCalls) > 0 {
		message.ToolCalls = s.toolCalls(in.Message.ToolCalls, false)
	}
	usage := usageOf(in)
	out := chatCompletion{ID: s.id, Object: "chat.completion", Created: s.created, Model: s.model,
		Choices: []outputChoice{{Index: 0, Message: message, FinishReason: finishReason(in.DoneReason, s.toolCount > 0)}}, Usage: usageJSON(usage)}
	data, err := json.Marshal(out)
	return gateway.Event{Kind: gateway.EventData, Payload: data, Usage: usage,
		TextBytes: outputBytes(in.Message)}, err
}

func (s *responseStream) toolCalls(calls []nativeToolCall, streaming bool) []chatCall {
	out := make([]chatCall, 0, len(calls))
	for _, call := range calls {
		converted := chatCall{ID: "call_" + s.id + "_" + strconv.Itoa(s.toolCount), Type: "function",
			Function: chatFunction{Name: call.Function.Name, Arguments: string(call.Function.Arguments)}}
		if streaming {
			index := s.toolCount
			converted.Index = &index
		}
		s.toolCount++
		out = append(out, converted)
	}
	return out
}

func usageOf(in nativeResponse) *gateway.Usage {
	if in.PromptEvalCount == nil || in.EvalCount == nil {
		return nil
	}
	return &gateway.Usage{PromptTokens: *in.PromptEvalCount, CompletionTokens: *in.EvalCount}
}

func usageJSON(usage *gateway.Usage) *chatUsage {
	if usage == nil {
		return nil
	}
	return &chatUsage{PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		TotalTokens: usage.PromptTokens + usage.CompletionTokens}
}

func outputBytes(msg nativeMessage) int {
	total := len(msg.Content) + len(msg.Thinking)
	for _, call := range msg.ToolCalls {
		total += len(call.Function.Arguments)
	}
	return total
}

func finishReason(reason string, tools bool) string {
	if tools {
		return "tool_calls"
	}
	if reason == "length" {
		return "length"
	}
	return "stop"
}

func hasOutput(msg nativeMessage) bool {
	return msg.Content != "" || msg.Thinking != "" || len(msg.ToolCalls) > 0
}

func invalidResponse(message string) gateway.Event {
	return gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{
		Status: http.StatusBadGateway, Type: "upstream_error", Code: "invalid_response", Message: message,
	}}
}

func emptyResponse() gateway.Event {
	return gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{
		Status: http.StatusBadGateway, Type: "upstream_error", Code: "empty_response", Message: "Ollama returned no output",
	}}
}
