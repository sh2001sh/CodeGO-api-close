package gateway

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// applyPathStructuralOperation executes the path-based operations that
// rewrite the JSON body structurally (as opposed to transforming an
// existing string value in place). terminal reports whether err must be
// returned to the caller as-is instead of being wrapped with
// "operation %s failed: %w" by applyOperations.
func applyPathStructuralOperation(result []byte, op ParamOperation, opPaths []string, contextJSON string) (newResult []byte, err error, terminal bool) {
	switch op.Mode {
	case "delete":
		return applyDeleteOperation(result, opPaths)
	case "set":
		return applySetOperation(result, op, opPaths)
	case "move":
		opFrom := processNegativeIndex(result, op.From)
		opTo := processNegativeIndex(result, op.To)
		newResult, err = moveValue(result, opFrom, opTo)
		return newResult, err, false
	case "copy":
		if op.From == "" || op.To == "" {
			return nil, fmt.Errorf("copy from/to is required"), true
		}
		opFrom := processNegativeIndex(result, op.From)
		opTo := processNegativeIndex(result, op.To)
		newResult, err = copyValue(result, opFrom, opTo)
		return newResult, err, false
	case "prepend":
		return applyModifyValueOperation(result, op, opPaths, true)
	case "append":
		return applyModifyValueOperation(result, op, opPaths, false)
	case "prune_objects":
		return applyPruneObjectsOperation(result, op, opPaths, contextJSON)
	default:
		return result, fmt.Errorf("unknown operation: %s", op.Mode), true
	}
}

func applyDeleteOperation(result []byte, opPaths []string) ([]byte, error, bool) {
	for left, right := 0, len(opPaths)-1; left < right; left, right = left+1, right-1 {
		opPaths[left], opPaths[right] = opPaths[right], opPaths[left]
	}
	var err error
	for _, path := range opPaths {
		result, err = deleteValue(result, path)
		if err != nil {
			break
		}
	}
	return result, err, false
}

func applySetOperation(result []byte, op ParamOperation, opPaths []string) ([]byte, error, bool) {
	var err error
	for _, path := range opPaths {
		if op.KeepOrigin && gjson.GetBytes(result, path).Exists() {
			continue
		}
		result, err = sjson.SetBytes(result, path, op.Value)
		if err != nil {
			break
		}
	}
	return result, err, false
}

func applyModifyValueOperation(result []byte, op ParamOperation, opPaths []string, isPrepend bool) ([]byte, error, bool) {
	var err error
	for _, path := range opPaths {
		result, err = modifyValue(result, path, op.Value, op.KeepOrigin, isPrepend)
		if err != nil {
			break
		}
	}
	return result, err, false
}

func applyPruneObjectsOperation(result []byte, op ParamOperation, opPaths []string, contextJSON string) ([]byte, error, bool) {
	var err error
	for _, path := range opPaths {
		result, err = pruneObjects(result, path, contextJSON, op.Value)
		if err != nil {
			break
		}
	}
	return result, err, false
}

// applyPathTransformOperation executes the path-based operations that
// transform an existing string value in place.
func applyPathTransformOperation(result []byte, op ParamOperation, opPaths []string) ([]byte, error) {
	transform, ok := stringPathTransform(op.Mode)
	if ok {
		return applyTransformStringOperation(result, opPaths, transform)
	}

	var err error
	for _, path := range opPaths {
		switch op.Mode {
		case "trim_prefix":
			result, err = trimStringValue(result, path, op.Value, true)
		case "trim_suffix":
			result, err = trimStringValue(result, path, op.Value, false)
		case "ensure_prefix":
			result, err = ensureStringAffix(result, path, op.Value, true)
		case "ensure_suffix":
			result, err = ensureStringAffix(result, path, op.Value, false)
		case "replace":
			result, err = replaceStringValue(result, path, op.From, op.To)
		case "regex_replace":
			result, err = regexReplaceStringValue(result, path, op.From, op.To)
		}
		if err != nil {
			break
		}
	}
	return result, err
}

func applyTransformStringOperation(result []byte, opPaths []string, transform func(string) string) ([]byte, error) {
	var err error
	for _, path := range opPaths {
		result, err = transformStringValue(result, path, transform)
		if err != nil {
			break
		}
	}
	return result, err
}

func stringPathTransform(mode string) (func(string) string, bool) {
	switch mode {
	case "trim_space":
		return strings.TrimSpace, true
	case "to_lower":
		return strings.ToLower, true
	case "to_upper":
		return strings.ToUpper, true
	default:
		return nil, false
	}
}
