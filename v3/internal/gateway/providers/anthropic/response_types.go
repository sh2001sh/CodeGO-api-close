package anthropic

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type wireUsage struct {
	Input      *int64 `json:"input_tokens"`
	Output     *int64 `json:"output_tokens"`
	CacheRead  *int64 `json:"cache_read_input_tokens"`
	CacheWrite *int64 `json:"cache_creation_input_tokens"`
	Creation   struct {
		OneHour *int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	ServerTools struct {
		WebSearch *int64 `json:"web_search_requests"`
		WebFetch  *int64 `json:"web_fetch_requests"`
	} `json:"server_tool_use"`
}

type wireBlock struct {
	Type     string          `json:"type"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
}

type wireMessage struct {
	ID         string      `json:"id"`
	Model      string      `json:"model"`
	Content    []wireBlock `json:"content"`
	StopReason string      `json:"stop_reason"`
	Usage      *wireUsage  `json:"usage"`
	Error      *wireError  `json:"error"`
}

type wireError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type wireEvent struct {
	Type         string      `json:"type"`
	Index        int         `json:"index"`
	Message      wireMessage `json:"message"`
	ContentBlock wireBlock   `json:"content_block"`
	Usage        *wireUsage  `json:"usage"`
	Delta        struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Error *wireError `json:"error"`
}

type usageState struct {
	input, output, cacheRead, cacheWrite, oneHour int64
	present                                       bool
	webSearch, webFetch                           int64
}

func (u *usageState) merge(w *wireUsage) (*gateway.Usage, error) {
	if w != nil {
		for _, item := range []struct {
			src *int64
			dst *int64
		}{
			{w.Input, &u.input}, {w.Output, &u.output}, {w.CacheRead, &u.cacheRead},
			{w.CacheWrite, &u.cacheWrite}, {w.Creation.OneHour, &u.oneHour},
			{w.ServerTools.WebSearch, &u.webSearch}, {w.ServerTools.WebFetch, &u.webFetch},
		} {
			if item.src != nil {
				*item.dst = *item.src
				u.present = true
			}
		}
	}
	if !u.present {
		return nil, nil
	}
	if u.input < 0 || u.output < 0 || u.cacheRead < 0 || u.cacheWrite < 0 || u.oneHour < 0 || u.oneHour > u.cacheWrite || u.webSearch < 0 || u.webFetch < 0 {
		return nil, fmt.Errorf("anthropic: invalid token usage")
	}
	if u.input > math.MaxInt64-u.cacheRead || u.input+u.cacheRead > math.MaxInt64-u.cacheWrite {
		return nil, fmt.Errorf("anthropic: token usage overflow")
	}
	if u.output > math.MaxInt64-(u.input+u.cacheRead+u.cacheWrite) {
		return nil, fmt.Errorf("anthropic: total token usage overflow")
	}
	out := &gateway.Usage{PromptTokens: u.input + u.cacheRead + u.cacheWrite, CompletionTokens: u.output,
		CachedTokens: u.cacheRead, CacheWriteTokens: u.cacheWrite - u.oneHour, CacheWrite1hTokens: u.oneHour}
	if u.webSearch > 0 || u.webFetch > 0 {
		out.ToolCalls = make(map[string]int64)
		if u.webSearch > 0 {
			out.ToolCalls["web_search"] = u.webSearch
		}
		if u.webFetch > 0 {
			out.ToolCalls["web_fetch"] = u.webFetch
		}
	}
	return out, nil
}

func upstreamError(e *wireError) *gateway.UpstreamError {
	if e == nil {
		return nil
	}
	status := http.StatusBadGateway
	if e.Type == "rate_limit_error" {
		status = http.StatusTooManyRequests
	}
	code := e.Type
	if code == "" {
		code = "upstream_error"
	}
	return &gateway.UpstreamError{Status: status, Type: code, Code: code, Message: e.Message}
}

func decodeError(err error) gateway.Event {
	return gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{Status: http.StatusBadGateway,
		Type: "upstream_error", Code: "invalid_upstream_response", Message: err.Error()}}
}

func finishReason(reason string) string {
	switch reason {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}
