package gateway

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type syncTarget struct {
	kind string
	key  string
}

func parseSyncTarget(spec string) (syncTarget, error) {
	raw := strings.TrimSpace(spec)
	if raw == "" {
		return syncTarget{}, fmt.Errorf("sync_fields target is required")
	}

	idx := strings.Index(raw, ":")
	if idx < 0 {
		return syncTarget{
			kind: "json",
			key:  raw,
		}, nil
	}

	kind := strings.ToLower(strings.TrimSpace(raw[:idx]))
	key := strings.TrimSpace(raw[idx+1:])
	if key == "" {
		return syncTarget{}, fmt.Errorf("sync_fields target key is required: %s", raw)
	}

	switch kind {
	case "json", "body":
		return syncTarget{
			kind: "json",
			key:  key,
		}, nil
	case "header":
		return syncTarget{
			kind: "header",
			key:  key,
		}, nil
	default:
		return syncTarget{}, fmt.Errorf("sync_fields target prefix is invalid: %s", raw)
	}
}

func readSyncTargetValue(data []byte, context map[string]interface{}, target syncTarget) (interface{}, bool, error) {
	switch target.kind {
	case "json":
		path := processNegativeIndex(data, target.key)
		if negativeIndexRegexp.MatchString(path) {
			return nil, false, fmt.Errorf("array index out of range")
		}
		value := gjson.GetBytes(data, path)
		if !value.Exists() || value.Type == gjson.Null {
			return nil, false, nil
		}
		if value.Type == gjson.String && strings.TrimSpace(value.String()) == "" {
			return nil, false, nil
		}
		return value.Value(), true, nil
	case "header":
		value, ok := getHeaderValueFromContext(context, target.key)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, false, nil
		}
		return value, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported sync_fields target kind: %s", target.kind)
	}
}

func writeSyncTargetValue(data []byte, context map[string]interface{}, target syncTarget, value interface{}) ([]byte, error) {
	switch target.kind {
	case "json":
		path := processNegativeIndex(data, target.key)
		if negativeIndexRegexp.MatchString(path) {
			return nil, fmt.Errorf("array index out of range")
		}
		nextJSON, err := sjson.SetBytes(data, path, value)
		if err != nil {
			return nil, err
		}
		return nextJSON, nil
	case "header":
		if err := setHeaderOverrideInContext(context, target.key, value, false); err != nil {
			return nil, err
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported sync_fields target kind: %s", target.kind)
	}
}

func syncFieldsBetweenTargets(data []byte, context map[string]interface{}, fromSpec string, toSpec string) ([]byte, error) {
	fromTarget, err := parseSyncTarget(fromSpec)
	if err != nil {
		return nil, err
	}
	toTarget, err := parseSyncTarget(toSpec)
	if err != nil {
		return nil, err
	}

	fromValue, fromExists, err := readSyncTargetValue(data, context, fromTarget)
	if err != nil {
		return nil, err
	}
	toValue, toExists, err := readSyncTargetValue(data, context, toTarget)
	if err != nil {
		return nil, err
	}
	if fromExists && !toExists {
		return writeSyncTargetValue(data, context, toTarget, fromValue)
	}
	if toExists && !fromExists {
		return writeSyncTargetValue(data, context, fromTarget, toValue)
	}
	return data, nil
}
