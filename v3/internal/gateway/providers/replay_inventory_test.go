package providers_test

import "testing"

// This is the published v2 channel roster used by the importer. It deliberately
// distinguishes native async/vector/image channels from streaming Chat, rather
// than registering an OpenAI adapter to inflate the seven-terminal matrix.
func TestProviderReplayLegacyRoster(t *testing.T) {
	legacyIDs := []string{
		"openai", "azure", "ollama", "openai_max", "ohmygpt", "custom", "ails", "aiproxy", "palm", "api2gpt",
		"aigc2d", "anthropic", "baidu", "zhipu", "ali", "xunfei", "360", "openrouter", "aiproxy_library", "fastgpt",
		"tencent", "gemini", "moonshot", "zhipu_4v", "perplexity", "lingyiwanwu", "aws", "cohere", "minimax", "suno",
		"dify", "jina", "cloudflare", "siliconflow", "vertex", "mistral", "deepseek", "mokaai", "volcengine", "baidu_v2",
		"xinference", "xai", "coze", "kling", "jimeng", "vidu", "submodel", "doubao_video", "replicate", "codex",
	}
	applicability := map[string]string{
		"palm":         "one JSON response, no reported tokens; five feasible states covered by TestProviderReplayPaLMIntrinsicStates",
		"jina":         "embeddings and rerank; billed HTTP acceptance in TestProviderReplayNonChatOperations",
		"mokaai":       "embeddings only in v2; native /embeddings and text-array conversion in TestProviderReplayNonChatOperations",
		"replicate":    "image predictions and edits; billed HTTP image acceptance in TestProviderReplayNonChatOperations",
		"suno":         "async music/lyrics submit and poll; internal/workflow/providers/suno/provider_test.go",
		"kling":        "async video submit/poll JWT; internal/workflow/providers/kling/provider_test.go",
		"jimeng":       "async image/video signed submit and poll; internal/workflow/providers/jimeng/provider_test.go",
		"vidu":         "async video submit and poll; internal/workflow/providers/vidu/provider_test.go",
		"doubao_video": "async video submit and poll; internal/workflow/providers/doubao/provider_test.go",
	}
	fixtures := append(compatibleReplayFixtures(), nativeReplayFixtures()...)
	covered := make(map[string]bool)
	for _, fixture := range fixtures {
		covered[fixture.id] = true
	}
	if len(legacyIDs) != 50 {
		t.Fatalf("legacy roster changed to %d; reconcile importer IDs and applicability", len(legacyIDs))
	}
	streaming := 0
	for _, id := range legacyIDs {
		if reason := applicability[id]; reason != "" {
			t.Logf("%s applicability: %s", id, reason)
			continue
		}
		streaming++
		if !covered[id] {
			t.Errorf("legacy streaming provider %s has no real wire fixture", id)
		}
	}
	t.Logf("%d legacy IDs: %d streaming Chat providers, PaLM, 3 auxiliary-only providers, 5 async providers; %d native wire/alias/model-family combinations × 7 terminal states", len(legacyIDs), streaming, len(fixtures))
}
