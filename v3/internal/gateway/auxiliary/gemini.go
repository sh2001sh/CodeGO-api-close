package auxiliary

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type geminiNativeAdapter struct{}

func geminiAdapter() Adapter { return geminiNativeAdapter{} }

func (geminiNativeAdapter) Build(ctx context.Context, req *gateway.Request, target gateway.Target, input Input) (*http.Request, error) {
	switch input.Operation {
	case Embeddings, Images, GeminiEmbed, GeminiBatchEmbed, GeminiImages:
	default:
		return nil, unsupported(input.Operation)
	}
	model := strings.TrimPrefix(upstreamModel(req, target), "models/")
	if model == "" || strings.ContainsAny(model, "/?#%\\: \t\r\n") {
		return nil, geminiInvalid("invalid Gemini model")
	}
	body, err := object(input.Body)
	if err != nil {
		return nil, geminiInvalid(err.Error())
	}
	action, body, err := geminiBuildAction(input.Operation, body, model)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(target.BaseURL, "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	if !strings.HasSuffix(base, "/v1") && !strings.HasSuffix(base, "/v1beta") && !strings.HasSuffix(base, "/v1alpha") {
		base += "/v1beta"
	}
	address, err := endpoint(base, "models/"+model+":"+action)
	if err != nil {
		return nil, err
	}
	upstream, err := jsonRequest(ctx, address, "", body)
	if err == nil {
		upstream.Header.Set("X-Goog-Api-Key", target.Secret)
	}
	return upstream, err
}

// geminiBuildAction resolves the native Gemini API action for the given
// operation and rewrites body into that action's expected request shape.
func geminiBuildAction(op Operation, body map[string]json.RawMessage, model string) (string, map[string]json.RawMessage, error) {
	switch op {
	case Embeddings:
		body, err := geminiEmbeddingRequest(body, model)
		return "batchEmbedContents", body, err
	case Images:
		body, err := geminiImageRequest(body, model)
		return "predict", body, err
	case GeminiEmbed:
		if _, err := object(body["content"]); err != nil {
			return "", nil, geminiInvalid("content is required")
		}
		body["model"], _ = json.Marshal("models/" + model)
		return "embedContent", body, nil
	case GeminiBatchEmbed:
		if err := geminiPrepareBatchEmbed(body, model); err != nil {
			return "", nil, err
		}
		return "batchEmbedContents", body, nil
	case GeminiImages:
		if !strings.HasPrefix(model, "imagen") {
			return "", nil, geminiInvalid("only Imagen models support image prediction")
		}
		return "predict", body, nil
	default:
		return "", nil, unsupported(op)
	}
}

// geminiPrepareBatchEmbed validates and rewrites the "requests" array in
// place for a batchEmbedContents call, stamping each request with the
// resolved model.
func geminiPrepareBatchEmbed(body map[string]json.RawMessage, model string) error {
	var requests []map[string]json.RawMessage
	if json.Unmarshal(body["requests"], &requests) != nil || len(requests) == 0 {
		return geminiInvalid("requests must be a nonempty array")
	}
	for _, request := range requests {
		if request == nil {
			return geminiInvalid("batch embedding requests must be objects")
		}
		if _, err := object(request["content"]); err != nil {
			return geminiInvalid("content is required for every embedding")
		}
		request["model"], _ = json.Marshal("models/" + model)
	}
	body["requests"], _ = json.Marshal(requests)
	return nil
}

func geminiInvalid(message string) error {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "invalid_request", Message: message}
}

func geminiEmbeddingRequest(body map[string]json.RawMessage, model string) (map[string]json.RawMessage, error) {
	var inputs []string
	var text string
	if json.Unmarshal(body["input"], &text) == nil {
		inputs = []string{text}
	} else if json.Unmarshal(body["input"], &inputs) != nil || len(inputs) == 0 {
		return nil, geminiInvalid("embedding input must be text or a nonempty array of text")
	}
	var dimensions int
	if raw, ok := body["dimensions"]; ok {
		if json.Unmarshal(raw, &dimensions) != nil || dimensions <= 0 {
			return nil, geminiInvalid("dimensions must be a positive integer")
		}
		if model != "text-embedding-004" && model != "gemini-embedding-exp-03-07" && model != "gemini-embedding-001" {
			return nil, geminiInvalid("this Gemini embedding model does not support dimensions")
		}
	}
	var encoding string
	if raw, ok := body["encoding_format"]; ok {
		if json.Unmarshal(raw, &encoding) != nil || (encoding != "float" && encoding != "base64") {
			return nil, geminiInvalid("encoding_format must be float or base64")
		}
	}
	requests := make([]map[string]any, len(inputs))
	for i, input := range inputs {
		if strings.TrimSpace(input) == "" {
			return nil, geminiInvalid("embedding input cannot be empty")
		}
		requests[i] = map[string]any{"model": "models/" + model, "content": map[string]any{"parts": []map[string]string{{"text": input}}}}
		if dimensions > 0 {
			requests[i]["outputDimensionality"] = dimensions
		}
	}
	encoded, err := json.Marshal(requests)
	return map[string]json.RawMessage{"requests": encoded}, err
}
