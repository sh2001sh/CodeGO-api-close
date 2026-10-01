package gateway

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type pruneObjectsOptions struct {
	conditions []ConditionOperation
	logic      string
	recursive  bool
}

func pruneObjects(data []byte, path, contextJSON string, value interface{}) ([]byte, error) {
	options, err := parsePruneObjectsOptions(value)
	if err != nil {
		return nil, err
	}

	if path == "" {
		var root interface{}
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, err
		}
		cleaned, _, err := pruneObjectsNode(root, options, contextJSON, true)
		if err != nil {
			return nil, err
		}
		return json.Marshal(cleaned)
	}

	target := gjson.GetBytes(data, path)
	if !target.Exists() {
		return data, nil
	}

	var targetNode interface{}
	if target.Type == gjson.JSON {
		if err := unmarshalOverrideString(target.Raw, &targetNode); err != nil {
			return nil, err
		}
	} else {
		targetNode = target.Value()
	}

	cleaned, _, err := pruneObjectsNode(targetNode, options, contextJSON, true)
	if err != nil {
		return nil, err
	}
	cleanedBytes, err := json.Marshal(cleaned)
	if err != nil {
		return nil, err
	}
	return sjson.SetRawBytes(data, path, cleanedBytes)
}

func parsePruneObjectsOptions(value interface{}) (pruneObjectsOptions, error) {
	opts := pruneObjectsOptions{
		logic:     "AND",
		recursive: true,
	}

	var err error
	switch raw := value.(type) {
	case nil:
		return opts, fmt.Errorf("prune_objects value is required")
	case string:
		opts, err = parsePruneObjectsStringValue(opts, raw)
	case map[string]interface{}:
		opts, err = parsePruneObjectsMapValue(opts, raw)
	default:
		return opts, fmt.Errorf("prune_objects value must be string or object")
	}
	if err != nil {
		return opts, err
	}

	return validatePruneObjectsOptions(opts)
}

func parsePruneObjectsStringValue(opts pruneObjectsOptions, raw string) (pruneObjectsOptions, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return opts, fmt.Errorf("prune_objects value is required")
	}
	opts.conditions = []ConditionOperation{
		{
			Path:  "type",
			Mode:  "full",
			Value: v,
		},
	}
	return opts, nil
}

func parsePruneObjectsMapValue(opts pruneObjectsOptions, raw map[string]interface{}) (pruneObjectsOptions, error) {
	if logic, exists := raw["logic"]; exists {
		if text, ok := logic.(string); !ok || strings.TrimSpace(text) == "" {
			return opts, fmt.Errorf("invalid prune condition logic")
		}
	}
	if recursive, exists := raw["recursive"]; exists {
		if _, ok := recursive.(bool); !ok {
			return opts, fmt.Errorf("prune recursive must be boolean")
		}
	}
	if logic, ok := raw["logic"].(string); ok && strings.TrimSpace(logic) != "" {
		opts.logic = logic
	}
	if recursive, ok := raw["recursive"].(bool); ok {
		opts.recursive = recursive
	}

	if condRaw, exists := raw["conditions"]; exists {
		conditions, err := parseConditionOperations(condRaw)
		if err != nil {
			return opts, err
		}
		opts.conditions = append(opts.conditions, conditions...)
	}

	if whereRaw, exists := raw["where"]; exists {
		whereMap, ok := whereRaw.(map[string]interface{})
		if !ok {
			return opts, fmt.Errorf("prune_objects where must be object")
		}
		for key, val := range whereMap {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			opts.conditions = append(opts.conditions, ConditionOperation{
				Path:  key,
				Mode:  "full",
				Value: val,
			})
		}
	}

	if matchType, exists := raw["type"]; exists {
		opts.conditions = append(opts.conditions, ConditionOperation{
			Path:  "type",
			Mode:  "full",
			Value: matchType,
		})
	}
	return opts, nil
}

func validatePruneObjectsOptions(opts pruneObjectsOptions) (pruneObjectsOptions, error) {
	if len(opts.conditions) == 0 {
		return opts, fmt.Errorf("prune_objects conditions are required")
	}
	if !strings.EqualFold(opts.logic, "AND") && !strings.EqualFold(opts.logic, "OR") {
		return opts, fmt.Errorf("invalid prune condition logic")
	}
	for _, condition := range opts.conditions {
		if err := validateOverrideCondition(condition); err != nil {
			return opts, err
		}
	}
	return opts, nil
}

func pruneObjectsNode(node interface{}, options pruneObjectsOptions, contextJSON string, isRoot bool) (interface{}, bool, error) {
	switch value := node.(type) {
	case []interface{}:
		result := make([]interface{}, 0, len(value))
		for _, item := range value {
			next, drop, err := pruneObjectsNode(item, options, contextJSON, false)
			if err != nil {
				return nil, false, err
			}
			if drop {
				continue
			}
			result = append(result, next)
		}
		return result, false, nil
	case map[string]interface{}:
		shouldDrop, err := shouldPruneObject(value, options, contextJSON)
		if err != nil {
			return nil, false, err
		}
		if shouldDrop && !isRoot {
			return nil, true, nil
		}
		if !options.recursive {
			return value, false, nil
		}
		for key, child := range value {
			next, drop, err := pruneObjectsNode(child, options, contextJSON, false)
			if err != nil {
				return nil, false, err
			}
			if drop {
				delete(value, key)
				continue
			}
			value[key] = next
		}
		return value, false, nil
	default:
		return node, false, nil
	}
}

func shouldPruneObject(node map[string]interface{}, options pruneObjectsOptions, contextJSON string) (bool, error) {
	nodeBytes, err := json.Marshal(node)
	if err != nil {
		return false, err
	}
	return checkConditions(nodeBytes, contextJSON, options.conditions, options.logic)
}
