package auxiliary

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/jina"
)

type vectorAdapter struct{ provider string }

func vectorAdapters() map[string]Adapter {
	adapters := make(map[string]Adapter)
	for _, id := range []string{"ollama", "cohere", "ali", "jina", "cloudflare", "baidu_v2", "zhipu", "zhipu_4v", "mokaai", "volcengine", "minimax"} {
		adapters[id] = vectorAdapter{provider: id}
	}
	adapters["baidu"] = &vectorBaiduAdapter{}
	return adapters
}

func (a vectorAdapter) supports(op Operation) bool {
	switch a.provider {
	case "ollama", "zhipu_4v", "mokaai", "volcengine":
		return op == Embeddings
	case "cloudflare":
		return op == Embeddings || op == Transcriptions || op == Translations || op == Completions
	case "cohere":
		return op == Rerank
	case "ali", "jina":
		return op == Embeddings || op == Rerank
	default:
		// v2 has no implemented native conversion for these operations.
		return false
	}
}

func (a vectorAdapter) Build(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	if !a.supports(in.Operation) {
		return nil, unsupported(in.Operation)
	}
	if req == nil {
		return nil, vectorInvalid("request is required")
	}
	if upstreamModel(req, target) == "" {
		return nil, vectorInvalid("model is required")
	}
	if request, handled, err := a.buildSpecialized(ctx, req, target, in); handled {
		return request, err
	}
	return a.buildEmbedding(ctx, req, target, in)
}

// buildSpecialized dispatches to the operation/provider-specific builders
// that don't follow the generic embeddings request shape. handled reports
// whether one of those builders applies, so the caller knows whether to fall
// through to buildEmbedding.
func (a vectorAdapter) buildSpecialized(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, bool, error) {
	switch {
	case a.provider == "cloudflare" && (in.Operation == Transcriptions || in.Operation == Translations):
		request, err := buildCloudflareAudio(ctx, req, target, in)
		return request, true, err
	case a.provider == "cloudflare" && in.Operation == Completions:
		request, err := buildCloudflareCompletions(ctx, req, target, in)
		return request, true, err
	case in.Operation == Embeddings && a.provider == "ollama":
		request, err := buildOllamaVectors(ctx, req, target, in)
		return request, true, err
	case in.Operation == Rerank && a.provider == "ali":
		request, err := buildAliRerank(ctx, req, target, in)
		return request, true, err
	case in.Operation == Rerank && a.provider == "cohere":
		request, err := buildCohereRerank(ctx, req, target, in)
		return request, true, err
	case a.provider == "jina":
		copyReq := *req
		copyReq.Body = in.Body
		if in.Operation == Rerank {
			request, err := jina.BuildRerankRequest(ctx, &copyReq, target)
			return request, true, err
		}
		request, err := jina.BuildEmbeddingRequest(ctx, &copyReq, target)
		return request, true, err
	default:
		return nil, false, nil
	}
}

// buildEmbedding builds the generic OpenAI-compatible embeddings request
// shared by the remaining providers, applying each provider's path/base URL
// and payload quirks.
func (a vectorAdapter) buildEmbedding(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	fields, err := object(in.Body)
	if err != nil {
		return nil, vectorInvalid("request must be a JSON object")
	}
	if err = validateVectorInput(fields["input"]); err != nil {
		return nil, err
	}
	model := upstreamModel(req, target)
	if model == "" {
		return nil, vectorInvalid("model is required")
	}
	fields["model"], _ = json.Marshal(model)
	path, secret, base := "", target.Secret, target.BaseURL
	switch a.provider {
	case "ali":
		path = "/compatible-mode/v1/embeddings"
		base = strings.TrimSuffix(strings.TrimRight(base, "/"), "/compatible-mode/v1")
	case "zhipu_4v":
		path = "/api/paas/v4/embeddings"
		base = strings.TrimRight(base, "/")
		if strings.HasSuffix(base, "/api/coding/paas/v4") {
			path = "/embeddings"
		} else {
			base = strings.TrimSuffix(base, "/api/paas/v4")
		}
	case "mokaai":
		path = "/embeddings"
		inputs, inputErr := vectorStrings(fields["input"])
		if inputErr != nil {
			return nil, inputErr
		}
		fields["input"], _ = json.Marshal(inputs)
	case "volcengine":
		path = "/api/v3/embeddings"
		base = strings.TrimSuffix(strings.TrimRight(base, "/"), "/api/v3")
	case "cloudflare":
		base, secret, err = vectorCloudflareBase(base, secret)
		if err != nil {
			return nil, err
		}
		path = "/v1/embeddings"
	}
	address, err := endpoint(base, path)
	if err != nil {
		return nil, err
	}
	return jsonRequest(ctx, address, secret, fields)
}

func vectorInvalid(message string) error {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "invalid_vector_request", Message: message}
}

func vectorUpstream(code, message string) error {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}

func validateVectorInput(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return vectorInvalid("input is required")
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return vectorInvalid("invalid input")
	}
	switch input := value.(type) {
	case string:
		if input != "" {
			return nil
		}
	case []any:
		if len(input) > 0 {
			return nil
		}
	}
	return vectorInvalid("input must be nonempty text or an array")
}

func vectorStrings(raw json.RawMessage) ([]string, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil && text != "" {
		return []string{text}, nil
	}
	var list []string
	if json.Unmarshal(raw, &list) != nil || len(list) == 0 {
		return nil, vectorInvalid("this provider requires nonempty text input")
	}
	for _, text := range list {
		if text == "" {
			return nil, vectorInvalid("input text must not be empty")
		}
	}
	return list, nil
}

func vectorFields(body []byte, allowed ...string) (map[string]json.RawMessage, error) {
	fields, err := object(body)
	if err != nil {
		return nil, vectorInvalid("request must be a JSON object")
	}
	for key, value := range fields {
		if string(value) == "null" {
			delete(fields, key)
			continue
		}
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
				break
			}
		}
		if !found {
			return nil, vectorInvalid("unsupported field: " + key)
		}
	}
	return fields, nil
}
