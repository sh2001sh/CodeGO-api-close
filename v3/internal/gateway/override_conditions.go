package gateway

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

func checkConditions(data []byte, contextJSON string, conditions []ConditionOperation, logic string) (bool, error) {
	if len(conditions) == 0 {
		return true, nil // 没有条件，直接通过
	}
	results := make([]bool, len(conditions))
	for i, condition := range conditions {
		result, err := checkSingleCondition(data, contextJSON, condition)
		if err != nil {
			return false, err
		}
		results[i] = result
	}

	if strings.ToUpper(logic) == "AND" {
		for _, result := range results {
			if !result {
				return false, nil
			}
		}
		return true, nil
	}
	for _, result := range results {
		if result {
			return true, nil
		}
	}
	return false, nil
}

func checkSingleCondition(data []byte, contextJSON string, condition ConditionOperation) (bool, error) {
	path := processNegativeIndex(data, condition.Path)
	value := gjson.GetBytes(data, path)
	if protectedOverrideContextPath(condition.Path) {
		value = gjson.Get(contextJSON, condition.Path)
	} else if !value.Exists() && contextJSON != "" {
		value = gjson.Get(contextJSON, condition.Path)
	}
	if !value.Exists() {
		if condition.PassMissingKey {
			return true, nil
		}
		return false, nil
	}
	targetBytes, err := json.Marshal(condition.Value)
	if err != nil {
		return false, fmt.Errorf("failed to marshal condition value: %v", err)
	}
	targetValue := gjson.ParseBytes(targetBytes)

	result, err := compareGjsonValues(value, targetValue, strings.ToLower(condition.Mode))
	if err != nil {
		return false, fmt.Errorf("comparison failed for path %s: %v", condition.Path, err)
	}

	if condition.Invert {
		result = !result
	}
	return result, nil
}

// Caller-provided JSON cannot impersonate the routing principal or reintroduce
// authentication headers into the sanitized condition context.
func protectedOverrideContextPath(path string) bool {
	root, _, _ := strings.Cut(path, ".")
	switch root {
	case "user_id", "token_id", "key_id", "user_group", "using_group", "group", "protocol", "original_model", "upstream_model", "request_path", "request_headers", "header_override", "retry_index", "is_retry", "retry", "last_error", "last_error_status_code", "last_error_message", "last_error_code", "last_error_type", "is_channel_test":
		return true
	}
	return false
}

func processNegativeIndex(data []byte, path string) string {
	result := path
	searchStart := 0
	for searchStart < len(result) {
		match := negativeIndexRegexp.FindStringSubmatchIndex(result[searchStart:])
		if match == nil {
			break
		}
		start, end := searchStart+match[0], searchStart+match[1]
		index, _ := strconv.Atoi(result[searchStart+match[2] : searchStart+match[3]])
		array := gjson.GetBytes(data, result[:start])
		if array.IsArray() {
			length := len(array.Array())
			actualIndex := length + index
			if actualIndex >= 0 && actualIndex < length {
				replacement := "." + strconv.Itoa(actualIndex)
				result = result[:start] + replacement + result[end:]
				searchStart = start + len(replacement)
				continue
			}
		}
		searchStart = end
	}

	return result
}
func compareGjsonValues(jsonValue, targetValue gjson.Result, mode string) (bool, error) {
	switch mode {
	case "full":
		return compareEqual(jsonValue, targetValue)
	case "prefix":
		return strings.HasPrefix(jsonValue.String(), targetValue.String()), nil
	case "suffix":
		return strings.HasSuffix(jsonValue.String(), targetValue.String()), nil
	case "contains":
		return strings.Contains(jsonValue.String(), targetValue.String()), nil
	case "gt":
		return compareNumeric(jsonValue, targetValue, "gt")
	case "gte":
		return compareNumeric(jsonValue, targetValue, "gte")
	case "lt":
		return compareNumeric(jsonValue, targetValue, "lt")
	case "lte":
		return compareNumeric(jsonValue, targetValue, "lte")
	default:
		return false, fmt.Errorf("unsupported comparison mode: %s", mode)
	}
}

func compareEqual(jsonValue, targetValue gjson.Result) (bool, error) {
	if jsonValue.Type == gjson.Null || targetValue.Type == gjson.Null {
		return jsonValue.Type == gjson.Null && targetValue.Type == gjson.Null, nil
	}
	if (jsonValue.Type == gjson.True || jsonValue.Type == gjson.False) &&
		(targetValue.Type == gjson.True || targetValue.Type == gjson.False) {
		return jsonValue.Bool() == targetValue.Bool(), nil
	}
	if jsonValue.Type != targetValue.Type {
		return false, fmt.Errorf("compare for different types, got %v and %v", jsonValue.Type, targetValue.Type)
	}

	switch jsonValue.Type {
	case gjson.True, gjson.False:
		return jsonValue.Bool() == targetValue.Bool(), nil
	case gjson.Number:
		return jsonValue.Num == targetValue.Num, nil
	case gjson.String:
		return jsonValue.String() == targetValue.String(), nil
	default:
		return jsonValue.String() == targetValue.String(), nil
	}
}

func compareNumeric(jsonValue, targetValue gjson.Result, operator string) (bool, error) {
	if jsonValue.Type != gjson.Number || targetValue.Type != gjson.Number {
		return false, fmt.Errorf("numeric comparison requires both values to be numbers, got %v and %v", jsonValue.Type, targetValue.Type)
	}

	jsonNum := jsonValue.Num
	targetNum := targetValue.Num

	switch operator {
	case "gt":
		return jsonNum > targetNum, nil
	case "gte":
		return jsonNum >= targetNum, nil
	case "lt":
		return jsonNum < targetNum, nil
	case "lte":
		return jsonNum <= targetNum, nil
	default:
		return false, fmt.Errorf("unsupported numeric operator: %s", operator)
	}
}
