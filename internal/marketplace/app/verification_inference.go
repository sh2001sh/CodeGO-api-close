package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func probeMarketplaceInference(provider, baseURL, apiKey, model string) error {
	_, err := probeMarketplaceInferenceTimed(provider, baseURL, apiKey, model)
	return err
}

func probeMarketplaceInferenceTimed(provider, baseURL, apiKey, model string) (int64, error) {
	endpoint, payload, err := inferenceProbeRequest(provider, baseURL, model)
	if err != nil {
		return 0, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	setMarketplaceAuthHeaders(req, provider, apiKey)
	startedAt := time.Now()
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("实际推理请求失败: %w", err)
	}
	defer response.Body.Close()
	latencyMS := time.Since(startedAt).Milliseconds()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return latencyMS, nil
	}
	detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	message := strings.TrimSpace(string(detail))
	if message == "" {
		message = response.Status
	}
	return latencyMS, fmt.Errorf("实际推理检测未通过（HTTP %d）: %s", response.StatusCode, message)
}

func probeMarketplaceInferenceReportedModel(provider, baseURL, apiKey, model string) (int64, string, error) {
	return probeMarketplaceInferenceReportedModelWithVariant(
		provider, baseURL, apiKey, model, gpt56ProbeVariant{
			Name: "default", Prompt: "Reply with OK.", MaxOutputTokens: 8,
		},
	)
}

func probeMarketplaceInferenceReportedModelWithVariant(
	provider, baseURL, apiKey, model string,
	variant gpt56ProbeVariant,
) (int64, string, error) {
	endpoint, payload, err := inferenceProbeRequestWithPrompt(
		provider, baseURL, model, variant.Prompt, variant.MaxOutputTokens,
	)
	if err != nil {
		return 0, "", err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	setMarketplaceAuthHeaders(req, provider, apiKey)
	startedAt := time.Now()
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("实际推理请求失败: %w", err)
	}
	defer response.Body.Close()
	latencyMS := time.Since(startedAt).Milliseconds()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		message := strings.TrimSpace(string(detail))
		if message == "" {
			message = response.Status
		}
		return latencyMS, "", fmt.Errorf("实际推理检测未通过（HTTP %d）: %s", response.StatusCode, message)
	}
	var payloadResponse struct {
		Model        string `json:"model"`
		ModelVersion string `json:"modelVersion"`
		Response     struct {
			Model string `json:"model"`
		} `json:"response"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payloadResponse); err != nil {
		return latencyMS, "", errors.New("上游推理响应格式无效")
	}
	for _, reported := range []string{payloadResponse.Model, payloadResponse.Response.Model, payloadResponse.ModelVersion} {
		if value := strings.TrimSpace(reported); value != "" {
			return latencyMS, value, nil
		}
	}
	return latencyMS, "", nil
}

func inferenceProbeRequest(provider, baseURL, model string) (string, map[string]any, error) {
	return inferenceProbeRequestWithPrompt(provider, baseURL, model, "Reply with OK.", 8)
}

func inferenceProbeRequestWithPrompt(
	provider, baseURL, model, prompt string,
	maxOutputTokens int,
) (string, map[string]any, error) {
	if maxOutputTokens <= 0 {
		maxOutputTokens = 8
	}
	switch provider {
	case "codex":
		endpoint, err := apiEndpoint(baseURL, "/v1/responses")
		return endpoint, map[string]any{
			"model": model, "input": prompt, "max_output_tokens": maxOutputTokens,
		}, err
	case "anthropic":
		endpoint, err := apiEndpoint(baseURL, "/v1/messages")
		return endpoint, map[string]any{
			"model": model, "max_tokens": maxOutputTokens,
			"messages": []map[string]string{{"role": "user", "content": prompt}},
		}, err
	case "gemini":
		endpoint, err := geminiInferenceEndpoint(baseURL, model)
		return endpoint, map[string]any{
			"contents":         []map[string]any{{"parts": []map[string]string{{"text": prompt}}}},
			"generationConfig": map[string]any{"maxOutputTokens": maxOutputTokens},
		}, err
	default:
		endpoint, err := apiEndpoint(baseURL, "/v1/chat/completions")
		return endpoint, map[string]any{
			"model": model, "max_tokens": maxOutputTokens,
			"messages": []map[string]string{{"role": "user", "content": prompt}},
		}, err
	}
}

func apiEndpoint(baseURL, targetPath string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Host == "" {
		return "", errors.New("Base URL 无效")
	}
	if strings.HasSuffix(parsed.Path, "/v1") && strings.HasPrefix(targetPath, "/v1/") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/v1") + targetPath
	} else {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + targetPath
	}
	return parsed.String(), nil
}

func geminiInferenceEndpoint(baseURL, model string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Host == "" {
		return "", errors.New("Base URL 无效")
	}
	basePath := strings.TrimSuffix(parsed.Path, "/v1beta")
	parsed.Path = strings.TrimRight(basePath, "/") + "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	return parsed.String(), nil
}
