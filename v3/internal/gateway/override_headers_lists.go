package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func splitHeaderListValue(raw string) []string {
	result := []string{}
	for _, token := range strings.Split(raw, ",") {
		if token = strings.TrimSpace(token); token != "" {
			result = append(result, token)
		}
	}
	return result
}

func parseHeaderReplacementTokens(value any) ([]string, error) {
	switch raw := value.(type) {
	case nil:
		return nil, nil
	case string:
		return splitHeaderListValue(raw), nil
	case []string:
		result := []string{}
		for _, item := range raw {
			result = append(result, splitHeaderListValue(item)...)
		}
		return uniqueOverrideStrings(result), nil
	case []any:
		result := []string{}
		for _, item := range raw {
			tokens, err := parseHeaderReplacementTokens(item)
			if err != nil {
				return nil, err
			}
			result = append(result, tokens...)
		}
		return uniqueOverrideStrings(result), nil
	case map[string]any, map[string]string:
		return nil, errors.New("header replacement must be string, array or null")
	default:
		return splitHeaderListValue(fmt.Sprint(value)), nil
	}
}

func parseHeaderPassThroughNames(value any) ([]string, error) {
	var names []string
	switch raw := value.(type) {
	case string:
		if text := strings.TrimSpace(raw); strings.HasPrefix(text, "[") || strings.HasPrefix(text, "{") {
			var parsed any
			if json.Unmarshal([]byte(text), &parsed) != nil {
				return nil, errors.New("invalid pass_headers JSON")
			}
			return parseHeaderPassThroughNames(parsed)
		}
		names = splitHeaderListValue(raw)
	case []string:
		names = raw
	case []any:
		for _, item := range raw {
			name, ok := item.(string)
			if !ok {
				return nil, errors.New("pass_headers names must be strings")
			}
			names = append(names, name)
		}
	case map[string]any:
		for _, key := range []string{"headers", "names", "header"} {
			if v, ok := raw[key]; ok {
				next, err := parseHeaderPassThroughNames(v)
				if err != nil {
					return nil, err
				}
				names = append(names, next...)
			}
		}
	default:
		return nil, errors.New("pass_headers must be string, array or object")
	}
	if len(names) == 0 {
		return nil, errors.New("pass_headers names are required")
	}
	for i, name := range names {
		names[i] = normalizeHeaderContextKey(name)
		if !validOverrideHeaderName(names[i]) {
			return nil, errors.New("invalid passthrough header name")
		}
	}
	return uniqueOverrideStrings(names), nil
}
