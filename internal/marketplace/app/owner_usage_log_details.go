package app

import (
	"encoding/json"
	"strconv"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
)

func applyOwnerUsageLogDetails(item *OwnerUsageLogItem, log auditschema.Log) {
	if item == nil {
		return
	}
	item.UpstreamRequestID = log.UpstreamRequestId
	item.TotalDurationMs = int64(log.UseTime) * 1000

	var other map[string]interface{}
	if json.Unmarshal([]byte(log.Other), &other) != nil {
		if log.Type == auditschema.LogTypeError {
			item.ErrorMessage = log.Content
		}
		return
	}
	item.FirstByteMs = firstInt64(other, "e2e_ttft_ms", "frt")
	item.AttemptTTFTMs = firstInt64(other, "attempt_ttft_ms")
	if duration := firstInt64(other, "total_duration_ms"); duration > 0 {
		item.TotalDurationMs = duration
	}
	item.StatusCode = int(firstInt64(other, "status_code"))
	item.ErrorType = stringValue(other["error_type"])
	item.ErrorCode = stringValue(other["error_code"])
	item.RequestPath = stringValue(other["request_path"])
	item.RetryCount = int(firstInt64(other, "retry_count"))
	if trace, ok := other["first_byte_trace"].(map[string]interface{}); ok {
		item.FirstByteTrace = trace
	}
	if log.Type == auditschema.LogTypeError {
		item.ErrorMessage = stringValue(other["owner_error"])
		if item.ErrorMessage == "" {
			item.ErrorMessage = log.Content
		}
	}
}

func firstInt64(values map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		if value := int64Value(values[key]); value != 0 {
			return value
		}
	}
	return 0
}

func int64Value(value interface{}) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		result, _ := typed.Int64()
		return result
	case string:
		result, _ := strconv.ParseInt(typed, 10, 64)
		return result
	default:
		return 0
	}
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
