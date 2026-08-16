package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestGPT56MappingEligibilityRequiresCodexSourceAndAllModels(t *testing.T) {
	channel := &marketplaceschema.Channel{
		SubmittedSourceLabel: "Codex Plus",
		DeclaredModels:       `["gpt-5.6-sol","gpt-5.6-terra","gpt-5.6-luna"]`,
	}
	require.True(t, isGPT56MappingEligible(channel))

	channel.SubmittedSourceLabel = "CC-Max"
	require.False(t, isGPT56MappingEligible(channel))

	channel.SubmittedSourceLabel = "Codex Pro"
	channel.DeclaredModels = `["gpt-5.6-sol","gpt-5.6-terra"]`
	require.False(t, isGPT56MappingEligible(channel))
}

func TestGPT56MappingStatusPrefersMismatch(t *testing.T) {
	require.Equal(t, GPT56MappingStatusMatched, gpt56MappingStatus([]GPT56MappingResult{
		{Status: GPT56MappingStatusMatched},
		{Status: GPT56MappingStatusMatched},
		{Status: GPT56MappingStatusMatched},
	}))
	require.Equal(t, GPT56MappingStatusMismatch, gpt56MappingStatus([]GPT56MappingResult{
		{Status: GPT56MappingStatusMatched},
		{Status: GPT56MappingStatusMismatch},
		{Status: GPT56MappingStatusInsufficientEvidence},
	}))
	require.Equal(t, GPT56MappingStatusInsufficientEvidence, gpt56MappingStatus([]GPT56MappingResult{
		{Status: GPT56MappingStatusMatched},
		{Status: GPT56MappingStatusInsufficientEvidence},
	}))
}

func TestGPT56MappingProbeReadsReportedModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var requestBody struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&requestBody))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"gpt-5.6-sol","choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer server.Close()

	latencyMS, reported, err := probeMarketplaceInferenceReportedModel(
		"openai_compatible", server.URL, "test-key", "gpt-5.6-sol",
	)
	require.NoError(t, err)
	require.GreaterOrEqual(t, latencyMS, int64(0))
	require.Equal(t, "gpt-5.6-sol", reported)
}
