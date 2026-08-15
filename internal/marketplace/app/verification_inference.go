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

func inferenceProbeRequest(provider, baseURL, model string) (string, map[string]any, error) {
	switch provider {
	case "codex":
		endpoint, err := apiEndpoint(baseURL, "/v1/responses")
		return endpoint, map[string]any{
			"model": model, "input": "Reply with OK.", "max_output_tokens": 8,
		}, err
	case "anthropic":
		endpoint, err := apiEndpoint(baseURL, "/v1/messages")
		return endpoint, map[string]any{
			"model": model, "max_tokens": 8,
			"messages": []map[string]string{{"role": "user", "content": "Reply with OK."}},
		}, err
	case "gemini":
		endpoint, err := geminiInferenceEndpoint(baseURL, model)
		return endpoint, map[string]any{
			"contents": []map[string]any{{"parts": []map[string]string{{"text": "Reply with OK."}}}},
		}, err
	default:
		endpoint, err := apiEndpoint(baseURL, "/v1/chat/completions")
		return endpoint, map[string]any{
			"model": model, "max_tokens": 8,
			"messages": []map[string]string{{"role": "user", "content": "Reply with OK."}},
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
