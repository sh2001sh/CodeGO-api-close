package ionet

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// decodeWithFlexibleTimes unmarshals API responses while tolerating timestamp strings
// that omit timezone information by normalizing them to RFC3339Nano.
func decodeWithFlexibleTimes(data []byte, target interface{}) error {
	var intermediate interface{}
	if err := json.Unmarshal(data, &intermediate); err != nil {
		return err
	}

	normalized := normalizeTimeValues(intermediate)
	reencoded, err := json.Marshal(normalized)
	if err != nil {
		return err
	}

	return json.Unmarshal(reencoded, target)
}

func decodeData[T any](data []byte, target *T) error {
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	if len(wrapper.Data) == 0 || string(wrapper.Data) == "null" {
		return errors.New("provider response has no data")
	}
	return json.Unmarshal(wrapper.Data, target)
}

func decodeDataWithFlexibleTimes[T any](data []byte, target *T) error {
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	if len(wrapper.Data) == 0 || string(wrapper.Data) == "null" {
		return errors.New("provider response has no data")
	}
	return decodeWithFlexibleTimes(wrapper.Data, target)
}

func normalizeTimeValues(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return mapValues(v, func(val interface{}, _ string) interface{} {
			return normalizeTimeValues(val)
		})
	case []interface{}:
		return mapSlice(v, func(item interface{}, _ int) interface{} {
			return normalizeTimeValues(item)
		})
	case string:
		if normalized, changed := normalizeTimeString(v); changed {
			return normalized
		}
		return v
	default:
		return value
	}
}

func normalizeTimeString(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return input, false
	}

	if _, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return trimmed, trimmed != input
	}
	if _, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return trimmed, trimmed != input
	}

	layouts := []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
	}

	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano), true
		}
	}

	return input, false
}
