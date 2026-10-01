package auxiliary

import (
	"context"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func defaultAdapters() map[string]Adapter {
	adapters := make(map[string]Adapter)
	for _, id := range []string{"openai", "custom", "openai_max", "openaimax", "ohmygpt", "ails", "aiproxy", "api2gpt", "aigc2d", "360", "openrouter", "aiproxy_library", "fastgpt", "lingyiwanwu", "xinference", "moonshot", "deepseek", "mistral", "submodel", "siliconflow", "xai", "responses"} {
		adapters[id] = compatibleAdapter{kind: id}
	}
	adapters["azure"] = compatibleAdapter{kind: "azure"}
	adapters["codex"] = compatibleAdapter{kind: "codex"}
	adapters["gemini"] = geminiAdapter()
	adapters["vertex"] = vertexAdapter()
	vectors, media := vectorAdapters(), mediaAdapters()
	for id, adapter := range vectors {
		adapters[id] = adapter
	}
	for id, adapter := range media {
		if vector := vectors[id]; vector != nil {
			adapters[id] = operationAdapter{vectors: vector, media: adapter}
		} else {
			adapters[id] = adapter
		}
	}
	adapters["zhipu_v4"] = adapters["zhipu_4v"]
	return adapters
}

type operationAdapter struct{ vectors, media Adapter }

func (a operationAdapter) selectAdapter(op Operation) Adapter {
	if op == Embeddings || op == Rerank {
		return a.vectors
	}
	return a.media
}

func (a operationAdapter) Build(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	return a.selectAdapter(in.Operation).Build(ctx, req, target, in)
}

func (a operationAdapter) Decode(ctx context.Context, req *gateway.Request, target gateway.Target, in Input, resp *http.Response) (Response, error) {
	return a.selectAdapter(in.Operation).Decode(ctx, req, target, in, resp)
}
