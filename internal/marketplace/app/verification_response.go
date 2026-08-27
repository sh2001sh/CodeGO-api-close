package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
)

func validateMarketplaceProbeResponse(provider, model string, body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		if gatewaycontract.IsImageGenerationModel(model) {
			return errors.New("生图请求已返回成功状态，但没有返回生成结果")
		}
		return nil
	}
	if err := detectMarketplaceProbeError(trimmed); err != nil {
		return err
	}
	var payload any
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		if !gatewaycontract.IsImageGenerationModel(model) {
			return errors.New("上游推理响应不是有效 JSON")
		}
		return errors.New("生图请求返回了无法识别的响应格式")
	}
	if !gatewaycontract.IsImageGenerationModel(model) {
		return validateTextProbePayload(provider, payload)
	}
	if !containsGeneratedImage(payload) {
		return errors.New("生图请求未返回图片 URL 或图片数据，不能判定为成功")
	}
	return nil
}

func validateTextProbePayload(provider string, value any) error {
	payload, ok := value.(map[string]any)
	if !ok {
		return errors.New("上游推理响应格式无效")
	}
	var containers []any
	switch provider {
	case "codex":
		containers = append(containers, payload["output_text"], payload["output"])
	case "anthropic":
		containers = append(containers, payload["content"])
	case "gemini":
		containers = append(containers, payload["candidates"])
	default:
		containers = append(containers, payload["choices"])
	}
	for _, container := range containers {
		output := strings.TrimSpace(probeOutputText(container))
		if output == "" {
			continue
		}
		if failure := probeFailureText(output); failure != "" {
			return fmt.Errorf("探针模型返回失败内容: %s", failure)
		}
		return nil
	}
	if hasCompletedProbeEnvelope(provider, payload) {
		return nil
	}
	return errors.New("上游推理响应缺少有效的模型输出")
}

func hasCompletedProbeEnvelope(provider string, payload map[string]any) bool {
	switch provider {
	case "codex":
		status, _ := payload["status"].(string)
		return strings.EqualFold(strings.TrimSpace(status), "completed")
	case "anthropic":
		stopReason, _ := payload["stop_reason"].(string)
		return strings.TrimSpace(stopReason) != ""
	case "gemini":
		return arrayContainsCompletionMarker(payload["candidates"], "finishReason")
	default:
		return arrayContainsCompletionMarker(payload["choices"], "finish_reason")
	}
}

func arrayContainsCompletionMarker(value any, marker string) bool {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return false
	}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if completion, ok := entry[marker].(string); ok && strings.TrimSpace(completion) != "" {
			return true
		}
	}
	return false
}

func probeOutputText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(probeOutputText(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	case map[string]any:
		parts := make([]string, 0)
		for key, item := range typed {
			switch strings.ToLower(strings.ReplaceAll(key, "_", "")) {
			case "message", "content", "text", "outputtext", "parts", "candidates", "choices", "output", "response":
				if text := strings.TrimSpace(probeOutputText(item)); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func probeFailureText(message string) string {
	message = strings.TrimSpace(message)
	normalized := strings.ToLower(message)
	if normalized == "failed" || normalized == "failure" || normalized == "error" ||
		strings.HasPrefix(normalized, "error:") || strings.HasPrefix(normalized, "failed:") ||
		strings.Contains(message, "请求失败") || strings.Contains(message, "生成失败") {
		return message
	}
	return ""
}

func detectMarketplaceProbeError(body []byte) error {
	var payload any
	if json.Unmarshal(body, &payload) != nil {
		message := strings.ToLower(strings.TrimSpace(string(body)))
		if message == "failed" || message == "error" || strings.HasPrefix(message, "error:") {
			return fmt.Errorf("上游返回失败内容: %s", strings.TrimSpace(string(body)))
		}
		return nil
	}
	message := probePayloadError(payload)
	if message == "" {
		return nil
	}
	return fmt.Errorf("上游返回失败内容: %s", message)
}

func probePayloadError(value any) string {
	payload, ok := value.(map[string]any)
	if !ok {
		if items, isArray := value.([]any); isArray {
			for _, item := range items {
				if message := probePayloadError(item); message != "" {
					return message
				}
			}
		}
		return ""
	}
	if success, exists := payload["success"].(bool); exists && !success {
		return probePayloadMessage(payload, "success=false")
	}
	if status, exists := payload["status"].(string); exists {
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "error", "failed", "failure", "cancelled", "canceled":
			return probePayloadMessage(payload, status)
		}
	}
	if errorValue, exists := payload["error"]; exists && errorValue != nil {
		if message := stringifyProbeError(errorValue); message != "" {
			return message
		}
		return "上游响应包含 error 字段"
	}
	for _, item := range payload {
		if message := probePayloadError(item); message != "" {
			return message
		}
	}
	return ""
}

func probePayloadMessage(payload map[string]any, fallback string) string {
	for _, key := range []string{"message", "detail", "error_message"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return fallback
}

func stringifyProbeError(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return probePayloadMessage(typed, "")
	default:
		return fmt.Sprint(typed)
	}
}

func containsGeneratedImage(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if containsGeneratedImage(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			normalizedKey := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			switch normalizedKey {
			case "url", "b64json", "imagebytes", "bytesbase64encoded":
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					return true
				}
			}
			if containsGeneratedImage(item) {
				return true
			}
		}
	}
	return false
}
