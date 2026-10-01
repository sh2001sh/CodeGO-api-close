package responses

import (
	"net/http"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func parseBody(body []byte) (gjson.Result, *gateway.UpstreamError) {
	root := gjson.ParseBytes(body)
	if !gjson.ValidBytes(body) || !root.IsObject() {
		return root, &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "invalid_upstream_response", Message: "upstream returned an invalid Responses event"}
	}
	return root, nil
}

func parseUsage(u gjson.Result) *gateway.Usage {
	if !u.IsObject() {
		return nil
	}
	return &gateway.Usage{
		PromptTokens: u.Get("input_tokens").Int(), CompletionTokens: u.Get("output_tokens").Int(),
		CachedTokens:      u.Get("input_tokens_details.cached_tokens").Int(),
		CacheWriteTokens:  cacheWriteTokens(u.Get("input_tokens_details")),
		ImageInputTokens:  u.Get("input_tokens_details.image_tokens").Int(),
		ImageOutputTokens: u.Get("output_tokens_details.image_tokens").Int(),
		AudioInputTokens:  u.Get("input_tokens_details.audio_tokens").Int(),
		AudioOutputTokens: u.Get("output_tokens_details.audio_tokens").Int(),
	}
}

func cacheWriteTokens(details gjson.Result) int64 {
	for _, key := range []string{"cached_creation_tokens", "cache_creation_tokens", "cache_creation_input_tokens", "cache_write_tokens", "cache_write_input_tokens"} {
		if n := details.Get(key).Int(); n > 0 {
			return n
		}
	}
	return 0
}

func responseError(root gjson.Result) *gateway.UpstreamError {
	e := root.Get("error")
	hasError := e.Exists() && e.Type != gjson.Null
	if root.Get("type").Str == "error" && !e.IsObject() {
		e = root // Responses also uses {"type":"error","code":...,"message":...}.
		hasError = true
	}
	if !hasError && root.Get("status").Str != "failed" && root.Get("type").Str != "response.failed" {
		return nil
	}
	code, message, typ := e.Get("code").String(), e.Get("message").Str, e.Get("type").Str
	if message == "" && hasError && !e.IsObject() {
		message = e.String()
	}
	if code == "" {
		code = "upstream_error"
	}
	if message == "" {
		message = "upstream response failed"
	}
	if typ == "" {
		typ = "upstream_error"
	}
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: typ, Code: code, Message: message}
}

func emptyError() *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "empty_response", Message: "upstream returned an empty response"}
}

func hasOutput(output gjson.Result) bool {
	found := false
	output.ForEach(func(_, item gjson.Result) bool {
		found = semanticItem(item)
		return !found
	})
	return found
}

func semanticItem(item gjson.Result) bool {
	if typ := item.Get("type").Str; typ != "" && typ != "message" && typ != "reasoning" {
		return true
	}
	found := false
	for _, path := range []string{"content", "summary"} {
		item.Get(path).ForEach(func(_, part gjson.Result) bool {
			found = part.Get("text").Str != "" || part.Get("refusal").Str != "" || part.Get("audio").Exists()
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

func outputTextBytes(output gjson.Result) int {
	count := 0
	output.ForEach(func(_, item gjson.Result) bool {
		count += len(item.Get("arguments").Str)
		for _, path := range []string{"content", "summary"} {
			item.Get(path).ForEach(func(_, part gjson.Result) bool {
				count += len(part.Get("text").Str) + len(part.Get("refusal").Str)
				return true
			})
		}
		return true
	})
	return count
}
