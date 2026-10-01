package gateway

import "fmt"

func applyOperations(jsonData []byte, operations []ParamOperation, conditionContext map[string]interface{}) ([]byte, error) {
	context := ensureContextMap(conditionContext)
	contextJSON, err := marshalContextJSON(context)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal condition context: %v", err)
	}

	result := jsonData
	for _, op := range operations {
		ok, err := checkConditions(result, contextJSON, op.Conditions, op.Logic)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue // 条件不满足，跳过当前操作
		}
		opPath := processNegativeIndex(result, op.Path)
		var opPaths []string
		if isPathBasedOperation(op.Mode) {
			opPaths, err = resolveOperationPaths(result, opPath)
			if err != nil {
				return nil, err
			}
			if len(opPaths) == 0 {
				continue
			}
		}

		var opErr error
		var terminal bool
		result, contextJSON, opErr, terminal = dispatchParamOperation(result, op, opPaths, context, contextJSON)
		if opErr != nil {
			if terminal {
				return nil, opErr
			}
			return nil, fmt.Errorf("operation %s failed: %w", op.Mode, opErr)
		}
	}
	return result, nil
}

// dispatchParamOperation executes a single ParamOperation and reports whether
// the resulting error (if any) is already final, i.e. must be returned as-is
// instead of being wrapped by applyOperations with "operation %s failed: %w".
func dispatchParamOperation(result []byte, op ParamOperation, opPaths []string, context map[string]interface{}, contextJSON string) ([]byte, string, error, bool) {
	switch op.Mode {
	case "delete", "set", "move", "copy", "prepend", "append", "prune_objects":
		newResult, err, terminal := applyPathStructuralOperation(result, op, opPaths, contextJSON)
		return newResult, contextJSON, err, terminal
	case "trim_prefix", "trim_suffix", "ensure_prefix", "ensure_suffix", "trim_space", "to_lower", "to_upper", "replace", "regex_replace":
		newResult, err := applyPathTransformOperation(result, op, opPaths)
		return newResult, contextJSON, err, false
	case "return_error":
		returnErr, parseErr := parseParamOverrideReturnError(op.Value)
		if parseErr != nil {
			return nil, contextJSON, parseErr, true
		}
		return nil, contextJSON, returnErr, true
	case "set_header", "delete_header", "copy_header", "move_header", "pass_headers":
		newContextJSON, err, terminal := applyHeaderContextOperation(op.Mode, context, op, contextJSON)
		return result, newContextJSON, err, terminal
	case "sync_fields":
		newResult, syncErr := syncFieldsBetweenTargets(result, context, op.From, op.To)
		if syncErr != nil {
			return nil, contextJSON, syncErr, false
		}
		newContextJSON, marshalErr := marshalContextJSON(context)
		return newResult, newContextJSON, marshalErr, false
	default:
		return nil, contextJSON, fmt.Errorf("unknown operation: %s", op.Mode), true
	}
}
