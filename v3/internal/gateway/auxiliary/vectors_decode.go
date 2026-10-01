package auxiliary

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func (a vectorAdapter) Decode(_ context.Context, req *gateway.Request, _ gateway.Target, in Input, resp *http.Response) (Response, error) {
	if !a.supports(in.Operation) {
		return Response{}, unsupported(in.Operation)
	}
	if a.provider == "cloudflare" && (in.Operation == Transcriptions || in.Operation == Translations) {
		return decodeCloudflareAudio(req, in, resp)
	}
	if a.provider == "cloudflare" && in.Operation == Completions {
		return decodeCloudflareCompletions(req, resp)
	}
	data, err := readResponse(resp)
	if err != nil {
		return Response{}, err
	}
	if err = vectorResponseError(data, resp.StatusCode); err != nil {
		return Response{}, err
	}
	if in.Operation == Rerank {
		return decodeVectorRerank(a.provider, data, resp.Header)
	}
	if a.provider == "ollama" {
		return decodeOllamaVectors(req, data, resp.Header)
	}
	if a.provider == "cloudflare" {
		root := gjson.ParseBytes(data)
		if result := root.Get("result"); result.IsObject() {
			data = []byte(result.Raw)
		}
	}
	return decodeCompatibleVectors(req, data, resp.Header)
}

func vectorResponseError(data []byte, status int) error {
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return vectorUpstream("invalid_response", "invalid vector response JSON")
	}
	root := gjson.ParseBytes(data)
	if status < 200 || status >= 300 {
		return &gateway.UpstreamError{Status: status, Type: "upstream_error", Code: "vector_upstream_error", Message: "vector upstream rejected the request"}
	}
	if value := root.Get("error"); value.Exists() && value.Type != gjson.Null && value.Raw != `""` {
		return vectorUpstream("vector_upstream_error", "vector upstream returned an error")
	}
	if root.Get("error_code").Int() != 0 || root.Get("error_msg").String() != "" || root.Get("code").String() != "" {
		return vectorUpstream("vector_upstream_error", "vector upstream returned an error")
	}
	if value := root.Get("success"); value.Type == gjson.False {
		return vectorUpstream("vector_upstream_error", "Cloudflare request failed")
	}
	return nil
}

func decodeOllamaVectors(req *gateway.Request, data []byte, header http.Header) (Response, error) {
	var body struct {
		Embeddings [][]float64 `json:"embeddings"`
		Tokens     *int64      `json:"prompt_eval_count"`
	}
	if json.Unmarshal(data, &body) != nil || len(body.Embeddings) == 0 {
		return Response{}, vectorUpstream("invalid_response", "Ollama returned no embeddings")
	}
	items := make([]map[string]any, len(body.Embeddings))
	for index, embedding := range body.Embeddings {
		if len(embedding) == 0 {
			return Response{}, vectorUpstream("invalid_response", "Ollama returned an empty embedding")
		}
		items[index] = map[string]any{"object": "embedding", "index": index, "embedding": embedding}
	}
	var usage *gateway.Usage
	if body.Tokens != nil {
		if *body.Tokens < 0 {
			return Response{}, vectorUpstream("invalid_usage", "negative vector token count")
		}
		usage = &gateway.Usage{PromptTokens: *body.Tokens}
	}
	return vectorJSONResponse(map[string]any{"object": "list", "data": items, "model": req.Model}, header, usage)
}

func decodeCompatibleVectors(req *gateway.Request, data []byte, header http.Header) (Response, error) {
	fields, err := object(data)
	if err != nil {
		return Response{}, vectorUpstream("invalid_response", "invalid vector response")
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(fields["data"], &items) != nil || len(items) == 0 {
		return Response{}, vectorUpstream("invalid_response", "upstream returned no embeddings")
	}
	for index, item := range items {
		if item == nil || len(item["embedding"]) == 0 {
			return Response{}, vectorUpstream("invalid_response", "upstream returned a malformed embedding")
		}
		embedding := gjson.ParseBytes(item["embedding"])
		if !embedding.IsArray() && embedding.Type != gjson.String {
			return Response{}, vectorUpstream("invalid_response", "invalid embedding representation")
		}
		if embedding.IsArray() {
			if len(embedding.Array()) == 0 {
				return Response{}, vectorUpstream("invalid_response", "upstream returned an empty embedding")
			}
			for _, number := range embedding.Array() {
				if number.Type != gjson.Number {
					return Response{}, vectorUpstream("invalid_response", "embedding contains nonnumeric values")
				}
			}
		} else if embedding.String() == "" {
			return Response{}, vectorUpstream("invalid_response", "upstream returned an empty embedding")
		}
		item["object"] = json.RawMessage(`"embedding"`)
		if len(item["index"]) == 0 {
			item["index"], _ = json.Marshal(index)
		}
	}
	fields["object"] = json.RawMessage(`"list"`)
	fields["model"], _ = json.Marshal(req.Model)
	fields["data"], _ = json.Marshal(items)
	usage := parseUsage(data)
	if err := vectorUsageError(usage); err != nil {
		return Response{}, err
	}
	if usage != nil {
		usageFields := make(map[string]json.RawMessage)
		if raw := fields["usage"]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &usageFields); err != nil {
				return Response{}, vectorUpstream("invalid_usage", "invalid vector usage object")
			}
		}
		for name, count := range map[string]int64{"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens, "total_tokens": usage.PromptTokens + usage.CompletionTokens} {
			usageFields[name], _ = json.Marshal(count)
		}
		fields["usage"], _ = json.Marshal(usageFields)
	}
	output, err := json.Marshal(fields)
	if err != nil {
		return Response{}, err
	}
	return Response{Body: output, Header: vectorJSONHeaders(header), Usage: usage}, nil
}

func decodeVectorRerank(provider string, data []byte, header http.Header) (Response, error) {
	root := gjson.ParseBytes(data)
	results := root.Get("results")
	if provider == "ali" {
		results = root.Get("output.results")
	}
	if !results.IsArray() || len(results.Array()) == 0 {
		return Response{}, vectorUpstream("invalid_response", "upstream returned no rerank results")
	}
	for _, result := range results.Array() {
		index, score := result.Get("index"), result.Get("relevance_score")
		if !result.IsObject() || index.Type != gjson.Number || index.Int() < 0 || index.Float() != float64(index.Int()) || score.Type != gjson.Number {
			return Response{}, vectorUpstream("invalid_response", "invalid rerank result")
		}
	}
	usage := parseUsage(data)
	if provider == "cohere" {
		units := root.Get("meta.billed_units")
		if units.Get("input_tokens").Exists() || units.Get("output_tokens").Exists() {
			usage = &gateway.Usage{PromptTokens: units.Get("input_tokens").Int(), CompletionTokens: units.Get("output_tokens").Int()}
		}
	}
	if err := vectorUsageError(usage); err != nil {
		return Response{}, err
	}
	body := map[string]any{"results": json.RawMessage(results.Raw)}
	// Preserve Cohere search_units even when token usage is absent; the caller
	// then estimates tokens instead of incorrectly reporting zero actual usage.
	if meta := root.Get("meta"); meta.IsObject() {
		body["meta"] = json.RawMessage(meta.Raw)
	}
	return vectorJSONResponse(body, header, usage)
}

func vectorUsageError(usage *gateway.Usage) error {
	if usage != nil && (usage.PromptTokens < 0 || usage.CompletionTokens < 0) {
		return vectorUpstream("invalid_usage", "negative vector token count")
	}
	return nil
}

func vectorJSONResponse(body map[string]any, header http.Header, usage *gateway.Usage) (Response, error) {
	if usage != nil {
		body["usage"] = map[string]int64{"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens, "total_tokens": usage.PromptTokens + usage.CompletionTokens}
	}
	data, err := json.Marshal(body)
	return Response{Body: data, Header: vectorJSONHeaders(header), Usage: usage}, err
}

func vectorJSONHeaders(original http.Header) http.Header {
	headers := original.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Del("Content-Length")
	headers.Del("Content-Encoding")
	headers.Set("Content-Type", "application/json")
	return headers
}
