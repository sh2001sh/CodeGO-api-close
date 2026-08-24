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
