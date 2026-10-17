package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	"github.com/stretchr/testify/require"
)

func TestInferenceProbeRequestUsesProviderProtocol(t *testing.T) {
	tests := []struct {
		provider string
		path     string
	}{
		{provider: "openai_compatible", path: "/v1/chat/completions"},
		{provider: "codex", path: "/v1/responses"},
		{provider: "anthropic", path: "/v1/messages"},
		{provider: "gemini", path: "/v1beta/models/gemini-2.5-pro:generateContent"},
	}
	for _, item := range tests {
		endpoint, payload, err := inferenceProbeRequest(item.provider, "https://api.example.com", "gemini-2.5-pro")
		require.NoError(t, err)
		require.True(t, strings.HasSuffix(endpoint, item.path), endpoint)
		require.NotEmpty(t, payload)
		if item.provider == "codex" {
			require.Equal(t, marketplaceProbeOutputTokens, payload["max_output_tokens"])
		}
	}
}

func TestImageProbeUsesGenerationEndpointAndLongTimeout(t *testing.T) {
	endpoint, payload, err := inferenceProbeRequest(
		"openai_compatible", "https://api.example.com", "gpt-image-1",
	)
	require.NoError(t, err)
	require.Equal(t, "https://api.example.com/v1/images/generations", endpoint)
	require.Equal(t, "gpt-image-1", payload["model"])
	require.GreaterOrEqual(t, marketplaceProbeTimeout("gpt-image-1"), marketplaceMinimumImageTimeout)
	require.Equal(t, marketplaceTextProbeTimeout, marketplaceProbeTimeout("gpt-5.6-sol"))
}

func TestGeminiNativeImageProbeUsesGenerateContentAndRequiresImageData(t *testing.T) {
	const model = "gemini-3.1-flash-image-preview"
	endpoint, payload, err := inferenceProbeRequestWithPrompt("gemini", "https://api.example.com/v1beta", model, "do not forward moderation text", 1024)
	require.NoError(t, err)
	require.Equal(t, "https://api.example.com/v1beta/models/"+model+":generateContent", endpoint)
	require.NotContains(t, endpoint, ":predict")
	require.Equal(t, []string{"TEXT", "IMAGE"}, payload["generationConfig"].(map[string]any)["responseModalities"])
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "do not forward moderation text")
	require.False(t, presentationProbeSupportsText(model))
	require.GreaterOrEqual(t, marketplaceProbeTimeout(model), marketplaceMinimumImageTimeout)
	endpoint, payload, err = inferenceProbeRequest("openai_compatible", "https://api.example.com/v1", model)
	require.NoError(t, err)
	require.Equal(t, "https://api.example.com/v1/chat/completions", endpoint)
	require.Equal(t, []string{"image", "text"}, payload["modalities"])
	require.NoError(t, validateMarketplaceProbeResponse("gemini", model, []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`)))
	require.NoError(t, validateMarketplaceProbeResponse("gemini", model, []byte(`{"candidates":[{"content":{"parts":[{"inline_data":{"mime_type":"image/png","data":"aW1hZ2U="}}]}}]}`)))
	for _, body := range []string{`{"candidates":[{"content":{"parts":[{"text":"SAFE"}]}}]}`, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"YXVkaW8="}}]}}]}`, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":""}}]}}]}`} {
		require.ErrorContains(t, validateMarketplaceProbeResponse("gemini", model, []byte(body)), "未返回图片")
	}
	endpoint, _, err = inferenceProbeRequest("gemini", "https://api.example.com/v1beta", "imagen-3.0-generate")
	require.NoError(t, err)
	require.Contains(t, endpoint, ":predict")
}

func TestProbeResponseRejectsEmbeddedFailureAndMissingImage(t *testing.T) {
	require.EqualError(
		t,
		validateMarketplaceProbeResponse("openai_compatible", "gpt-5.6-sol", []byte(`{"success":false,"message":"quota exceeded"}`)),
		"上游返回失败内容",
	)
	require.EqualError(
		t,
		validateMarketplaceProbeResponse("openai_compatible", "gpt-image-1", []byte(`{"created":123,"data":[]}`)),
		"生图请求未返回图片 URL 或图片数据，不能判定为成功",
	)
	require.NoError(t, validateMarketplaceProbeResponse(
		"openai_compatible", "gpt-image-1", []byte(`{"data":[{"b64_json":"aW1hZ2U="}]}`),
	))
}

func TestTextProbeRequiresProtocolOutputAndRejectsFailureReply(t *testing.T) {
	require.EqualError(t, validateMarketplaceProbeResponse(
		"openai_compatible", "gpt-5.6-sol", []byte(`{"choices":[]}`),
	), "上游推理响应缺少有效的模型输出")
	require.EqualError(t, validateMarketplaceProbeResponse(
		"openai_compatible", "gpt-5.6-sol",
		[]byte(`{"choices":[{"message":{"content":"请求失败，请稍后重试"}}]}`),
	), "探针模型返回失败内容")
	require.NoError(t, validateMarketplaceProbeResponse(
		"openai_compatible", "gpt-5.6-sol",
		[]byte(`{"choices":[{"message":{"content":"OK"}}]}`),
	))
	require.NoError(t, validateMarketplaceProbeResponse(
		"openai_compatible", "gpt-5.6-sol",
		[]byte(`{"choices":[{"message":{"content":null,"reasoning_content":"done"},"finish_reason":"length"}]}`),
	))
	require.NoError(t, validateMarketplaceProbeResponse(
		"codex", "gpt-5.6-sol", []byte(`{"status":"completed","output":[]}`),
	))
}

func TestInferenceProbeRetriesTransientHTTPFailure(t *testing.T) {
	requests := 0
	server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer server.Close()

	_, err := probeMarketplaceInferenceTimedContext(
		context.Background(), "openai_compatible", server.URL, "test-key", "gpt-5.6-sol",
	)
	require.NoError(t, err)
	require.Equal(t, 2, requests)
}

func TestInferenceProbeDoesNotRetryDeterministicHTTPFailure(t *testing.T) {
	requests := 0
	server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "invalid request", http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := probeMarketplaceInferenceTimedContext(
		context.Background(), "openai_compatible", server.URL, "test-key", "gpt-5.6-sol",
	)
	require.ErrorContains(t, err, "HTTP 400")
	require.Equal(t, 1, requests)
}

func TestInferenceProbeRejectsHTTP200ErrorPayload(t *testing.T) {
	server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"message":"model unavailable"}}`))
	}))
	defer server.Close()

	_, err := probeMarketplaceInferenceTimedContext(
		context.Background(), "openai_compatible", server.URL, "test-key", "gpt-5.6-sol",
	)
	require.EqualError(t, err, "上游返回失败内容")
}

func TestVerifyDeclaredModelsRejectsUnadvertisedModels(t *testing.T) {
	require.NoError(t, verifyDeclaredModels([]string{"gpt-5.2"}, []string{"gpt-5.2", "gpt-4.1"}))
	require.EqualError(t,
		verifyDeclaredModels([]string{"gpt-5.2", "missing-model"}, []string{"gpt-5.2"}),
		"声明模型未出现在上游模型列表: missing-model",
	)
}

func TestSelectProbeModelSkipsSyntheticReviewAlias(t *testing.T) {
	require.Equal(t, "gpt-5.5", selectProbeModel([]string{"codex-auto-review", "gpt-5.5"}))
}

func TestGeminiNativeImageDoesNotUseUnrelatedProviderProtocol(t *testing.T) {
	for _, provider := range []string{"codex", "anthropic"} {
		_, _, err := inferenceProbeRequest(provider, "https://example.com", "gemini-3.1-flash-image-preview")
		require.ErrorContains(t, err, "不支持 Gemini 原生生图协议")
	}
	for _, model := range []string{"gemini-2.0-flash-preview-image-generation", "gemini-2.0-flash-exp-image-generation"} {
		endpoint, _, err := inferenceProbeRequest("gemini", "https://example.com", model)
		require.NoError(t, err)
		require.Contains(t, endpoint, ":generateContent")
		require.NotContains(t, endpoint, ":predict")
	}
}

func TestProbeDeclaredModelsTestsEveryModel(t *testing.T) {
	tested := make([]string, 0, 2)
	server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		tested = append(tested, payload.Model)
		if payload.Model == "bad-model" {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer server.Close()

	var progressSnapshots int
	results, err := probeDeclaredModels(
		context.Background(),
		"openai_compatible", server.URL, "test-key",
		[]string{"good-model", "bad-model"}, []string{"good-model"},
		func(results []ModelVerificationResult) { progressSnapshots = len(results) },
	)
	require.EqualError(t, err, "模型连通性检测失败: bad-model")
	require.Equal(t, []string{"good-model", "bad-model", "bad-model"}, tested)
	require.Len(t, results, 2)
	require.Equal(t, marketplacedomain.ModelVerificationPassed, results[0].Status)
	require.True(t, results[0].Listed)
	require.Equal(t, marketplacedomain.ModelVerificationFailed, results[1].Status)
	require.False(t, results[1].Listed)
	require.NotEmpty(t, results[1].Error)
	require.Equal(t, 2, progressSnapshots)
}

func TestSelectVerifiedModelsRejectsUnlistedAndFailedModels(t *testing.T) {
	results := []ModelVerificationResult{
		{Model: "good-model", Status: marketplacedomain.ModelVerificationPassed, Listed: true},
		{Model: "failed-model", Status: marketplacedomain.ModelVerificationFailed, Listed: true},
		{Model: "unlisted-model", Status: marketplacedomain.ModelVerificationPassed, Listed: false},
	}

	passed, rejected := selectVerifiedModels(results)
	require.Equal(t, []string{"good-model"}, passed)
	require.Equal(t, []string{"failed-model", "unlisted-model"}, rejected)
}

func TestSelectVerifiedModelsRejectsAllFailedModels(t *testing.T) {
	results := []ModelVerificationResult{
		{Model: "failed-model", Status: marketplacedomain.ModelVerificationFailed, Listed: true},
		{Model: "unlisted-model", Status: marketplacedomain.ModelVerificationPassed, Listed: false},
	}

	passed, rejected := selectVerifiedModels(results)
	require.Empty(t, passed)
	require.Equal(t, []string{"failed-model", "unlisted-model"}, rejected)
}
