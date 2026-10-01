// Package providers assembles supported upstream adapters. Compatible aliases
// share one implementation; native protocols keep their own adapter.
package providers

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/ali"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/anthropic"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/azure"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/baidu"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bedrock"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bridge"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/cloudflare"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/codex"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/cohere"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/coze"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/dify"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/gemini"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/jina"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/ollama"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/palm"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/replicate"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/tencent"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/vertex"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/xunfei"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/zhipu"
)

// bridgeWrap pairs a chat-compatible adapter with its native-protocol
// adapters behind a single bridge.Provider.
type bridgeWrap func(chat gateway.Provider, native map[gateway.Protocol]gateway.Provider) gateway.Provider

func Registry(clients ...gateway.ClientProvider) map[string]gateway.Provider {
	wrap := bridgeWrap(func(chat gateway.Provider, native map[gateway.Protocol]gateway.Provider) gateway.Provider {
		return bridge.Provider{Chat: chat, Native: native}
	})
	nativeResponses := map[gateway.Protocol]gateway.Provider{gateway.ProtocolResponses: responses.Provider{}}
	images := registryImageTransport(clients)
	google := gemini.Provider{ImageTransport: images}
	aws := bedrock.Provider{ImageTransport: images}
	vertexProvider := vertex.Provider{ImageTransport: images, Clients: registryClientProvider(clients)}
	aliProvider := newAliProvider()

	registry := make(map[string]gateway.Provider)
	populateCompatibleProviders(registry, wrap, nativeResponses)
	populateNativeProtocolProviders(registry, wrap, nativeResponses, google, aws, aliProvider)
	populateSimpleProviders(registry, wrap)
	populateVertexAndCloudflare(registry, wrap, vertexProvider)
	populateOpenAICompatiblePaths(registry, wrap)
	registry["openaimax"] = registry["openai_max"]
	registry["zhipu_v4"] = registry["zhipu_4v"]
	return registry
}

// registryImageTransport adapts the gateway's upstream client provider into
// the http.RoundTripper form gemini/bedrock/vertex image fetches need.
func registryImageTransport(clients []gateway.ClientProvider) gemini.ImageTransport {
	if len(clients) == 0 || clients[0] == nil {
		return nil
	}
	return func(ctx context.Context, target gateway.Target) (http.RoundTripper, error) {
		client, err := clients[0](ctx, target)
		if err != nil || client == nil {
			return nil, err
		}
		return client.Transport, nil
	}
}

func registryClientProvider(clients []gateway.ClientProvider) gateway.ClientProvider {
	if len(clients) == 0 {
		return nil
	}
	return clients[0]
}

func newAliProvider() ali.Provider {
	aliProvider := ali.Provider{}
	if configured, exists := os.LookupEnv("ALI_ANTHROPIC_MESSAGES_MODELS"); exists {
		aliProvider.AnthropicModelPatterns = strings.Split(configured, ",")
	}
	return aliProvider
}

// populateCompatibleProviders registers the plain OpenAI-chat-compatible
// aliases, then upgrades the subset that also natively supports Responses.
func populateCompatibleProviders(registry map[string]gateway.Provider, wrap bridgeWrap, nativeResponses map[gateway.Protocol]gateway.Provider) {
	compatible := wrap(openai.Provider{}, nil)
	for _, id := range []string{"openai", "custom", "openai_max", "ohmygpt", "ails", "aiproxy", "api2gpt", "aigc2d", "360", "openrouter", "aiproxy_library", "fastgpt", "lingyiwanwu", "xinference", "xai", "moonshot", "deepseek", "mistral", "submodel", "siliconflow"} {
		registry[id] = compatible
	}
	for _, id := range []string{"openai", "custom", "openrouter", "xai"} {
		registry[id] = wrap(openai.Provider{}, nativeResponses)
	}
}

// populateNativeProtocolProviders registers providers whose native protocol
// adapters differ per instance (closures over google/aws/aliProvider), so
// they cannot be driven from a shared id/adapter table.
func populateNativeProtocolProviders(registry map[string]gateway.Provider, wrap bridgeWrap, nativeResponses map[gateway.Protocol]gateway.Provider, google gemini.Provider, aws bedrock.Provider, aliProvider ali.Provider) {
	registry[responses.ID] = wrap(responses.Provider{}, nativeResponses)
	registry[anthropic.ID] = wrap(anthropic.Provider{}, map[gateway.Protocol]gateway.Provider{gateway.ProtocolAnthropic: anthropic.Provider{}})
	registry["claude"] = registry[anthropic.ID]
	registry[gemini.ID] = wrap(google, map[gateway.Protocol]gateway.Provider{gateway.ProtocolGemini: google})
	registry[azure.ID] = wrap(azure.Provider{}, map[gateway.Protocol]gateway.Provider{gateway.ProtocolResponses: azure.Provider{}})
	registry[codex.ID] = wrap(codex.Provider{}, map[gateway.Protocol]gateway.Provider{gateway.ProtocolResponses: codex.Provider{}})
	registry[ali.ID] = wrap(aliProvider, map[gateway.Protocol]gateway.Provider{
		gateway.ProtocolResponses: aliProvider, gateway.ProtocolAnthropic: aliProvider,
	})
	registry[bedrock.ID] = wrap(aws, map[gateway.Protocol]gateway.Provider{gateway.ProtocolAnthropic: aws})
	registry["aws"] = registry[bedrock.ID]
}

// populateSimpleProviders registers providers that only need a chat adapter
// wrapped with no native protocol, plus the two bare (unwrapped) adapters.
func populateSimpleProviders(registry map[string]gateway.Provider, wrap bridgeWrap) {
	registry[ollama.ID] = wrap(ollama.Provider{}, nil)
	registry[cohere.ID] = wrap(cohere.Provider{}, nil)
	for id, adapter := range map[string]gateway.Provider{
		baidu.ID: baidu.Provider{}, palm.ID: palm.Provider{}, tencent.ID: tencent.Provider{},
		xunfei.ID: xunfei.Provider{}, dify.ID: dify.Provider{}, coze.ID: coze.Provider{},
		zhipu.ID: zhipu.Provider{},
	} {
		registry[id] = wrap(adapter, nil)
	}
	registry[jina.ID] = jina.Provider{}
	registry[replicate.ID] = replicate.Provider{}
}

func populateVertexAndCloudflare(registry map[string]gateway.Provider, wrap bridgeWrap, vertexProvider vertex.Provider) {
	registry[cloudflare.ID] = wrap(cloudflare.ChatProvider{}, map[gateway.Protocol]gateway.Provider{
		gateway.ProtocolResponses: cloudflare.ResponsesProvider{},
	})
	registry[vertex.ID] = wrap(vertexProvider, map[gateway.Protocol]gateway.Provider{
		gateway.ProtocolAnthropic: vertexProvider, gateway.ProtocolGemini: vertexProvider,
	})
}

// populateOpenAICompatiblePaths registers OpenAI-compatible providers that
// only differ by their chat completions path.
func populateOpenAICompatiblePaths(registry map[string]gateway.Provider, wrap bridgeWrap) {
	for id, path := range map[string]string{"perplexity": "/chat/completions", "zhipu_4v": "/api/paas/v4/chat/completions", "baidu_v2": "/v2/chat/completions", "volcengine": "/api/v3/chat/completions", "minimax": "/v1/chat/completions"} {
		registry[id] = wrap(openai.Provider{ChatPath: path, DisableStreamUsage: id == "perplexity"}, nil)
	}
}
