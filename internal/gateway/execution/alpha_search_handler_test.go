package execution

import (
	"testing"

	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"github.com/stretchr/testify/require"
)

func TestBuildAlphaSearchRequestBodyPreservesUnknownFields(t *testing.T) {
	raw := []byte(`{"model":"gpt-5.6-sol","commands":{"search_query":[{"q":"weather"}]},"future_field":{"nested":true}}`)

	result, err := buildAlphaSearchRequestBody(raw, "gpt-5.6-sol", "gpt-5.6-sol-mapped")
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, platformencoding.Unmarshal(result, &body))
	require.Equal(t, "gpt-5.6-sol-mapped", body["model"])
	require.Contains(t, body, "commands")
	require.Contains(t, body, "future_field")
}

func TestBuildAlphaSearchRequestBodyKeepsRawBytesWithoutMapping(t *testing.T) {
	raw := []byte(`{"model":"gpt-5.6-sol","query":"release notes"}`)
	result, err := buildAlphaSearchRequestBody(raw, "gpt-5.6-sol", "gpt-5.6-sol")
	require.NoError(t, err)
	require.Equal(t, raw, result)
}

func TestBuildPortableAlphaSearchResponsesBodyUsesStandardWebSearchTool(t *testing.T) {
	raw := []byte(`{
		"id":"search-1",
		"model":"gpt-6-astra",
		"commands":{"search_query":[{"q":"OpenAI Codex"}],"response_length":"short"},
		"settings":{"search_context_size":"high","filters":{"allowed_domains":["openai.com"]}},
		"max_output_tokens":1200
	}`)

	result, err := buildPortableAlphaSearchResponsesBody(raw, "gpt-6-astra-upstream")
	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"gpt-6-astra-upstream",
		"input":"Execute this standalone web search request using the web search tool. Follow every command in order and return the useful results with source URLs. Request JSON:\n{\n\t\t\"id\":\"search-1\",\n\t\t\"model\":\"gpt-6-astra\",\n\t\t\"commands\":{\"search_query\":[{\"q\":\"OpenAI Codex\"}],\"response_length\":\"short\"},\n\t\t\"settings\":{\"search_context_size\":\"high\",\"filters\":{\"allowed_domains\":[\"openai.com\"]}},\n\t\t\"max_output_tokens\":1200\n\t}",
		"tools":[{"type":"web_search","search_context_size":"high","filters":{"allowed_domains":["openai.com"]}}],
		"stream":false,
		"store":false,
		"max_output_tokens":1200
	}`, string(result))
}
