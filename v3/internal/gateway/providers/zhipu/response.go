package zhipu

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type chatMessage struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content"`
}

type chatChoice struct {
	Index        int          `json:"index"`
	Message      *chatMessage `json:"message,omitempty"`
	Delta        *chatMessage `json:"delta,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

func (s *responseStream) single() (gateway.Event, error) {
	s.ended = true
	data, err := io.ReadAll(io.LimitReader(s.body, maxBody+1))
	if err != nil {
		return gateway.Event{}, err
	}
	if len(data) > maxBody || !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "zhipu: invalid or oversized response JSON")}, nil
	}
	root := gjson.ParseBytes(data)
	if e := nativeError(root); e != nil {
		return gateway.Event{Kind: gateway.EventError, Err: e}, nil
	}
	if root.Get("success").Type != gjson.True {
		return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "zhipu: missing successful response envelope")}, nil
	}
	if e := statusError(root.Get("data.task_status").Str); e != nil {
		return gateway.Event{Kind: gateway.EventError, Err: e}, nil
	}
	usage, err := parseUsage(root.Get("data.usage"))
	if err != nil {
		return gateway.Event{}, err
	}
	s.updateID(root.Get("data"))
	choices := root.Get("data.choices")
	if !choices.IsArray() || len(choices.Array()) != 1 {
		return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "zhipu: expected one response choice"), Usage: usage}, nil
	}
	choice := choices.Array()[0]
	content := choice.Get("content")
	if content.Type != gjson.String || (choice.Get("role").Str != "assistant" && choice.Get("role").Str != "") {
		return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "zhipu: invalid assistant output"), Usage: usage}, nil
	}
	if content.Str == "" {
		return gateway.Event{Kind: gateway.EventError, Err: upstream("empty_response", "Zhipu returned no output"), Usage: usage}, nil
	}
	finish := "stop"
	payload, err := json.Marshal(chatResponse{ID: s.id, Object: "chat.completion", Created: s.created, Model: s.model,
		Choices: []chatChoice{{Message: &chatMessage{Role: "assistant", Content: content.Str}, FinishReason: &finish}}, Usage: usageJSON(usage)})
	return gateway.Event{Kind: gateway.EventData, Payload: payload, TextBytes: len(content.Str), Usage: usage}, err
}

func parseUsage(root gjson.Result) (*gateway.Usage, error) {
	if !root.Exists() || root.Type == gjson.Null {
		return nil, nil
	}
	if !root.IsObject() {
		return nil, errors.New("zhipu: usage must be an object")
	}
	values := make([]int64, 2)
	complete := true
	for i, field := range []string{"prompt_tokens", "completion_tokens"} {
		value := root.Get(field)
		if !value.Exists() || value.Type == gjson.Null {
			complete = false
			continue
		}
		count, err := strconv.ParseInt(value.Raw, 10, 64)
		if value.Type != gjson.Number || err != nil || count < 0 {
			return nil, errors.New("zhipu: invalid token count")
		}
		values[i] = count
	}
	if !complete {
		return nil, nil
	}
	if values[0] > 1<<63-1-values[1] {
		return nil, errors.New("zhipu: token count overflow")
	}
	return &gateway.Usage{PromptTokens: values[0], CompletionTokens: values[1]}, nil
}

func usageJSON(usage *gateway.Usage) *chatUsage {
	if usage == nil {
		return nil
	}
	return &chatUsage{PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, TotalTokens: usage.PromptTokens + usage.CompletionTokens}
}

func nativeError(root gjson.Result) *gateway.UpstreamError {
	code, msg := root.Get("code"), root.Get("msg").Str
	errorRoot := root.Get("error")
	if errorRoot.Exists() && errorRoot.Type != gjson.Null {
		code, msg = errorRoot.Get("code"), errorRoot.Get("message").Str
		if msg == "" && errorRoot.Type == gjson.String {
			msg = errorRoot.Str
		}
	} else if root.Get("success").Type != gjson.False && (!code.Exists() || code.Raw == "0" || code.Raw == "200") {
		return nil
	}
	if msg == "" {
		msg = root.Get("message").Str
	}
	if msg == "" {
		msg = "Zhipu reported an error"
	}
	errorCode := code.String()
	if errorCode == "" {
		errorCode = "zhipu_error"
	}
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: errorCode, Message: msg}
}

func statusError(status string) *gateway.UpstreamError {
	if status == "" || strings.EqualFold(status, "SUCCESS") {
		return nil
	}
	return upstream("zhipu_task_failed", "Zhipu did not complete its task: "+status)
}
