package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const (
	paramOverrideContextRequestHeaders = "request_headers"
	paramOverrideContextHeaderOverride = "header_override"
	paramOverrideDeletedHeaders        = "__deleted_headers"
)

var negativeIndexRegexp = regexp.MustCompile(`\.(-\d+)`)
var errSourceHeaderNotFound = errors.New("source header does not exist")

type ConditionOperation struct {
	Path           string `json:"path"`
	Mode           string `json:"mode"`
	Value          any    `json:"value"`
	Invert         bool   `json:"invert"`
	PassMissingKey bool   `json:"pass_missing_key"`
}

type ParamOperation struct {
	Path       string               `json:"path"`
	Mode       string               `json:"mode"`
	Value      any                  `json:"value"`
	KeepOrigin bool                 `json:"keep_origin"`
	From       string               `json:"from"`
	To         string               `json:"to"`
	Conditions []ConditionOperation `json:"conditions"`
	Logic      string               `json:"logic"`
}

// ParamOverrideReturnError retains an administrator's intentional rejection.
// Unwrap makes errors.As(err, **UpstreamError) work in every execution path.
type ParamOverrideReturnError struct {
	Upstream  *UpstreamError
	SkipRetry bool
}

func (e *ParamOverrideReturnError) Error() string { return e.Upstream.Error() }
func (e *ParamOverrideReturnError) Unwrap() error { return e.Upstream }

func parseOverrideOperations(value any) ([]ParamOperation, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if json.Unmarshal(encoded, &raw) != nil || raw == nil {
		return nil, errors.New("operations must be an array of objects")
	}
	operations := make([]ParamOperation, 0, len(raw))
	for _, item := range raw {
		if item == nil {
			return nil, errors.New("operation must be an object")
		}
		conditions, hasConditions := item["conditions"]
		copyItem := make(map[string]any, len(item))
		for k, v := range item {
			if k != "conditions" {
				copyItem[k] = v
			}
		}
		body, err := json.Marshal(copyItem)
		if err != nil {
			return nil, err
		}
		var op ParamOperation
		if err = json.Unmarshal(body, &op); err != nil {
			return nil, errors.New("invalid operation fields")
		}
		if hasConditions {
			if conditions == nil {
				return nil, errors.New("conditions must be an array or object")
			}
			op.Conditions, err = parseConditionOperations(conditions)
			if err != nil {
				return nil, err
			}
		}
		if err = validateOverrideOperation(op); err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}
	return operations, nil
}

func validateOverrideOperation(op ParamOperation) error {
	if op.Logic != "" && !strings.EqualFold(op.Logic, "AND") && !strings.EqualFold(op.Logic, "OR") {
		return errors.New("invalid condition logic")
	}
	for _, condition := range op.Conditions {
		if err := validateOverrideCondition(condition); err != nil {
			return err
		}
	}
	switch op.Mode {
	case "prune_objects", "return_error", "pass_headers":
	case "copy", "move", "sync_fields":
		if op.From == "" || op.To == "" {
			return errors.New("operation from/to is required")
		}
	case "copy_header", "move_header":
		if (op.From == "" || op.To == "") && op.Path == "" {
			return errors.New("header operation from/to is required")
		}
	case "delete", "set", "prepend", "append", "trim_prefix", "trim_suffix", "ensure_prefix", "ensure_suffix", "trim_space", "to_lower", "to_upper", "replace", "regex_replace", "set_header", "delete_header":
		if strings.TrimSpace(op.Path) == "" {
			return errors.New("operation path is required")
		}
	default:
		return fmt.Errorf("unknown override operation: %s", op.Mode)
	}
	return nil
}

func validateOverrideCondition(c ConditionOperation) error {
	if strings.TrimSpace(c.Path) == "" {
		return errors.New("condition path is required")
	}
	switch strings.ToLower(c.Mode) {
	case "full", "prefix", "suffix", "contains", "gt", "gte", "lt", "lte":
		return nil
	default:
		return errors.New("invalid condition comparison mode")
	}
}

func parseConditionOperations(raw any) ([]ConditionOperation, error) {
	result := []ConditionOperation{}
	switch value := raw.(type) {
	case map[string]any:
		if len(value) == 0 {
			return nil, errors.New("conditions object cannot be empty")
		}
		for path, item := range value {
			result = append(result, ConditionOperation{Path: path, Mode: "full", Value: item})
		}
	case []any:
		for _, item := range value {
			encoded, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			var condition ConditionOperation
			if json.Unmarshal(encoded, &condition) != nil {
				return nil, errors.New("condition must be an object")
			}
			result = append(result, condition)
		}
	default:
		return nil, errors.New("conditions must be an array or object")
	}
	for _, c := range result {
		if err := validateOverrideCondition(c); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func parseParamOverrideReturnError(value any) (*ParamOverrideReturnError, error) {
	upstream := &UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "invalid_request"}
	result := &ParamOverrideReturnError{Upstream: upstream, SkipRetry: true}
	switch raw := value.(type) {
	case string:
		upstream.Message = strings.TrimSpace(raw)
	case map[string]any:
		upstream.Message, _ = raw["message"].(string)
		if upstream.Message == "" {
			upstream.Message, _ = raw["msg"].(string)
		}
		if code, ok := raw["code"]; ok && code != nil {
			upstream.Code = fmt.Sprint(code)
		}
		if typ, ok := raw["type"].(string); ok && typ != "" {
			upstream.Type = typ
		}
		if skip, ok := raw["skip_retry"].(bool); ok {
			result.SkipRetry = skip
		}
		if skip, exists := raw["skip_retry"]; exists {
			if _, ok := skip.(bool); !ok {
				return nil, errors.New("return_error skip_retry must be boolean")
			}
		}
		status, present := raw["status_code"]
		if !present {
			status, present = raw["status"]
		}
		if present {
			var ok bool
			upstream.Status, ok = parseOverrideInt(status)
			if !ok {
				return nil, errors.New("return_error status must be an integer")
			}
		}
	default:
		return nil, errors.New("return_error value must be string or object")
	}
	if strings.TrimSpace(upstream.Message) == "" || upstream.Status < 100 || upstream.Status > 599 {
		return nil, errors.New("invalid return_error message or status")
	}
	return result, nil
}

func parseOverrideInt(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case float64:
		if n >= 100 && n <= 599 && n == float64(int(n)) {
			return int(n), true
		}
	case json.Number:
		parsed, err := strconv.Atoi(string(n))
		return parsed, err == nil
	}
	return 0, false
}

func unmarshalOverrideString(text string, value any) error {
	return json.Unmarshal([]byte(text), value)
}

func uniqueOverrideStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
