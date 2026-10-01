package auxiliary

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func buildOllamaVectors(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	fields, err := vectorFields(in.Body, "model", "input", "dimensions", "temperature", "top_p", "frequency_penalty", "presence_penalty", "seed", "options", "truncate", "keep_alive", "encoding_format")
	if err != nil {
		return nil, err
	}
	inputs, err := vectorStrings(fields["input"])
	if err != nil {
		return nil, err
	}
	if format := fields["encoding_format"]; len(format) > 0 && string(format) != `"float"` {
		return nil, vectorInvalid("ollama only returns float embeddings")
	}
	delete(fields, "encoding_format")
	fields["model"], _ = json.Marshal(upstreamModel(req, target))
	if len(inputs) == 1 {
		fields["input"], _ = json.Marshal(inputs[0])
	} else {
		fields["input"], _ = json.Marshal(inputs)
	}
	options := make(map[string]json.RawMessage)
	if original := fields["options"]; len(original) > 0 && json.Unmarshal(original, &options) != nil {
		return nil, vectorInvalid("ollama options must be an object")
	}
	if options == nil {
		return nil, vectorInvalid("ollama options must be an object")
	}
	for _, name := range []string{"dimensions", "temperature", "top_p", "frequency_penalty", "presence_penalty", "seed"} {
		if value := fields[name]; len(value) > 0 {
			options[name] = value
			if name != "dimensions" {
				delete(fields, name)
			}
		}
	}
	fields["options"], _ = json.Marshal(options)
	base := strings.TrimRight(target.BaseURL, "/")
	base = strings.TrimSuffix(strings.TrimSuffix(base, "/v1"), "/api")
	address, err := endpoint(base, "/api/embed")
	if err != nil {
		return nil, err
	}
	return jsonRequest(ctx, address, target.Secret, fields)
}

func vectorRerankFields(body []byte, extra ...string) (map[string]json.RawMessage, error) {
	allowed := append([]string{"model", "query", "documents", "top_n", "return_documents"}, extra...)
	fields, err := vectorFields(body, allowed...)
	if err != nil {
		return nil, err
	}
	var query string
	if json.Unmarshal(fields["query"], &query) != nil || strings.TrimSpace(query) == "" {
		return nil, vectorInvalid("query must be nonempty text")
	}
	var docs []json.RawMessage
	if json.Unmarshal(fields["documents"], &docs) != nil || len(docs) == 0 {
		return nil, vectorInvalid("documents must be a nonempty array")
	}
	if raw := fields["return_documents"]; len(raw) > 0 {
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return nil, vectorInvalid("return_documents must be boolean")
		}
	}
	if raw := fields["top_n"]; len(raw) > 0 {
		var value int
		if json.Unmarshal(raw, &value) != nil || value < 1 {
			return nil, vectorInvalid("top_n must be a positive integer")
		}
	}
	return fields, nil
}

func buildAliRerank(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	fields, err := vectorRerankFields(in.Body)
	if err != nil {
		return nil, err
	}
	parameters := make(map[string]json.RawMessage)
	parameters["return_documents"] = json.RawMessage("true")
	for _, key := range []string{"return_documents", "top_n"} {
		if value := fields[key]; len(value) > 0 {
			parameters[key] = value
		}
	}
	body := map[string]any{"model": upstreamModel(req, target), "input": map[string]json.RawMessage{"query": fields["query"], "documents": fields["documents"]}, "parameters": parameters}
	base := strings.TrimSuffix(strings.TrimRight(target.BaseURL, "/"), "/compatible-mode/v1")
	address, err := endpoint(base, "/api/v1/services/rerank/text-rerank/text-rerank")
	if err != nil {
		return nil, err
	}
	return jsonRequest(ctx, address, target.Secret, body)
}

func buildCohereRerank(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	fields, err := vectorRerankFields(in.Body, "max_chunks_per_doc", "rank_fields")
	if err != nil {
		return nil, err
	}
	fields["model"], _ = json.Marshal(upstreamModel(req, target))
	if len(fields["top_n"]) == 0 {
		fields["top_n"] = json.RawMessage("1")
	}
	if len(fields["return_documents"]) == 0 {
		fields["return_documents"] = json.RawMessage("true")
	}
	base := strings.TrimSuffix(strings.TrimRight(target.BaseURL, "/"), "/v1")
	address, err := endpoint(base, "/v1/rerank")
	if err != nil {
		return nil, err
	}
	return jsonRequest(ctx, address, target.Secret, fields)
}

func vectorCloudflareBase(base, secret string) (string, string, error) {
	var account, token string
	if strings.HasPrefix(strings.TrimSpace(secret), "{") {
		var credentials struct {
			Account string `json:"account_id"`
			Token   string `json:"token"`
			APIKey  string `json:"api_key"`
		}
		if json.Unmarshal([]byte(secret), &credentials) != nil {
			return "", "", vectorInvalid("invalid Cloudflare credential")
		}
		account, token = credentials.Account, credentials.Token
		if token == "" {
			token = credentials.APIKey
		}
	} else if parts := strings.Split(secret, "|"); len(parts) == 2 {
		account, token = parts[0], parts[1]
	} else {
		token = secret
	}
	base = strings.TrimRight(base, "/")
	if strings.Contains(base, "/accounts/") && strings.HasSuffix(base, "/ai") {
		if token == "" {
			return "", "", vectorInvalid("Cloudflare requires a token")
		}
		return base, token, nil
	}
	if account == "" || strings.ContainsAny(account, "/\\?#%") || token == "" {
		return "", "", vectorInvalid("Cloudflare requires account_id and token")
	}
	if base == "" {
		base = "https://api.cloudflare.com"
	}
	base = strings.TrimSuffix(base, "/client/v4")
	address, err := endpoint(base, "/client/v4/accounts/"+url.PathEscape(account)+"/ai")
	return address, token, err
}
