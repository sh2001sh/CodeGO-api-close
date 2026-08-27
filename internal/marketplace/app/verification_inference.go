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

	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

const (
	marketplaceTextProbeTimeout    = 60 * time.Second
	marketplaceMinimumImageTimeout = 180 * time.Second
	marketplaceProbeOutputTokens   = 32
	marketplaceProbeRetryDelay     = 250 * time.Millisecond
)

func probeMarketplaceInference(provider, baseURL, apiKey, model string) error {
	_, err := probeMarketplaceInferenceTimed(provider, baseURL, apiKey, model)
	return err
}

func probeMarketplaceInferenceTimed(provider, baseURL, apiKey, model string) (int64, error) {
	return probeMarketplaceInferenceTimedContext(context.Background(), provider, baseURL, apiKey, model)
}

func probeMarketplaceInferenceTimedContext(
	parent context.Context,
	provider, baseURL, apiKey, model string,
) (int64, error) {
	endpoint, payload, err := inferenceProbeRequest(provider, baseURL, model)
	if err != nil {
		return 0, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		latencyMS, transient, probeErr := probeMarketplaceInferenceAttempt(
			parent, provider, apiKey, model, endpoint, body,
		)
		if probeErr == nil || !transient || attempt == 1 || parent.Err() != nil {
			return latencyMS, probeErr
		}
		if err := waitMarketplaceProbeRetry(parent); err != nil {
			return latencyMS, err
		}
	}
	return 0, errors.New("实际推理检测失败")
}

func probeMarketplaceInferenceAttempt(
	parent context.Context,
	provider, apiKey, model, endpoint string,
	body []byte,
) (int64, bool, error) {
	timeout := marketplaceProbeTimeout(model)
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	setMarketplaceAuthHeaders(req, provider, apiKey)
	startedAt := time.Now()
	response, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, true, fmt.Errorf("%s探针等待 %s 后超时", marketplaceProbeKind(model), timeout)
		}
		return 0, parent.Err() == nil, fmt.Errorf("实际推理请求失败: %w", err)
	}
	defer response.Body.Close()
	latencyMS := time.Since(startedAt).Milliseconds()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 16<<20))
		if readErr != nil {
			return latencyMS, true, fmt.Errorf("读取上游响应失败: %w", readErr)
		}
		return latencyMS, false, validateMarketplaceProbeResponse(provider, model, responseBody)
	}
	detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	message := strings.TrimSpace(string(detail))
	if message == "" {
		message = response.Status
	}
	probeErr := fmt.Errorf("实际推理检测未通过（HTTP %d）: %s", response.StatusCode, message)
	return latencyMS, isTransientMarketplaceProbeStatus(response.StatusCode), probeErr
}

func isTransientMarketplaceProbeStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func waitMarketplaceProbeRetry(ctx context.Context) error {
	timer := time.NewTimer(marketplaceProbeRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func probeMarketplaceInferenceReportedModel(provider, baseURL, apiKey, model string) (int64, string, error) {
	return probeMarketplaceInferenceReportedModelWithVariant(
		provider, baseURL, apiKey, model, gpt56ProbeVariant{
			Name: "default", Prompt: "Reply with OK.", MaxOutputTokens: marketplaceProbeOutputTokens,
		},
	)
}

func probeMarketplaceInferenceReportedModelWithVariant(
	provider, baseURL, apiKey, model string,
	variant gpt56ProbeVariant,
) (int64, string, error) {
	return probeMarketplaceInferenceReportedModelWithVariantContext(
		context.Background(), provider, baseURL, apiKey, model, variant,
	)
}

func probeMarketplaceInferenceReportedModelWithVariantContext(
	parent context.Context,
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
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
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
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return latencyMS, "", fmt.Errorf("读取上游响应失败: %w", err)
	}
	if err := detectMarketplaceProbeError(bytes.TrimSpace(responseBody)); err != nil {
		return latencyMS, "", err
	}
	var payloadResponse struct {
		Model        string `json:"model"`
		ModelVersion string `json:"modelVersion"`
		Response     struct {
			Model string `json:"model"`
		} `json:"response"`
	}
	if err := json.Unmarshal(responseBody, &payloadResponse); err != nil {
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
	return inferenceProbeRequestWithPrompt(provider, baseURL, model, "Reply with OK.", marketplaceProbeOutputTokens)
}

func inferenceProbeRequestWithPrompt(
	provider, baseURL, model, prompt string,
	maxOutputTokens int,
) (string, map[string]any, error) {
	if gatewaycontract.IsImageGenerationModel(model) {
		return imageInferenceProbeRequest(provider, baseURL, model, prompt)
	}
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

func imageInferenceProbeRequest(provider, baseURL, model, prompt string) (string, map[string]any, error) {
	if provider == "gemini" {
		endpoint, err := geminiImageInferenceEndpoint(baseURL, model)
		return endpoint, map[string]any{
			"instances":  []map[string]string{{"prompt": prompt}},
			"parameters": map[string]any{"sampleCount": 1},
		}, err
	}
	endpoint, err := apiEndpoint(baseURL, "/v1/images/generations")
	return endpoint, map[string]any{
		"model": model, "prompt": prompt, "n": 1, "size": "1024x1024",
	}, err
}

func marketplaceProbeTimeout(model string) time.Duration {
	if !gatewaycontract.IsImageGenerationModel(model) {
		return marketplaceTextProbeTimeout
	}
	configured := time.Duration(platformconfig.ImageResponseHeaderTimeout) * time.Second
	if configured < marketplaceMinimumImageTimeout {
		return marketplaceMinimumImageTimeout
	}
	return configured
}

func marketplaceProbeKind(model string) string {
	if gatewaycontract.IsImageGenerationModel(model) {
		return "生图模型"
	}
	return "文本模型"
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

func geminiImageInferenceEndpoint(baseURL, model string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Host == "" {
		return "", errors.New("Base URL 无效")
	}
	basePath := strings.TrimSuffix(parsed.Path, "/v1beta")
	parsed.Path = strings.TrimRight(basePath, "/") + "/v1beta/models/" + url.PathEscape(model) + ":predict"
	return parsed.String(), nil
}
