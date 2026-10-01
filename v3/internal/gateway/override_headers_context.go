package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func ensureContextMap(conditionContext map[string]interface{}) map[string]interface{} {
	if conditionContext != nil {
		return conditionContext
	}
	return make(map[string]interface{})
}

func marshalContextJSON(context map[string]interface{}) (string, error) {
	if len(context) == 0 {
		return "", nil
	}
	ctxBytes, err := json.Marshal(context)
	if err != nil {
		return "", err
	}
	return string(ctxBytes), nil
}

func setHeaderOverrideInContext(context map[string]interface{}, headerName string, value interface{}, keepOrigin bool) error {
	headerName = normalizeHeaderContextKey(headerName)
	if !validOverrideHeaderName(headerName) {
		return fmt.Errorf("header name is required")
	}

	rawHeaders := ensureMapKeyInContext(context, paramOverrideContextHeaderOverride)
	if keepOrigin {
		if existing, ok := rawHeaders[headerName]; ok {
			existingValue := strings.TrimSpace(fmt.Sprintf("%v", existing))
			if existingValue != "" {
				return nil
			}
		}
	}

	headerValue, hasValue, err := resolveHeaderOverrideValue(context, headerName, value)
	if err != nil {
		return err
	}
	if !hasValue {
		delete(rawHeaders, headerName)
		return nil
	}

	rawHeaders[headerName] = headerValue
	delete(ensureMapKeyInContext(context, paramOverrideDeletedHeaders), headerName)
	return nil
}

func resolveHeaderOverrideValue(context map[string]interface{}, headerName string, value interface{}) (string, bool, error) {
	if value == nil {
		return "", false, fmt.Errorf("header value is required")
	}

	if mapping, ok := value.(map[string]interface{}); ok {
		return resolveHeaderOverrideValueByMapping(context, headerName, mapping)
	}
	if mapping, ok := value.(map[string]string); ok {
		converted := make(map[string]interface{}, len(mapping))
		for key, item := range mapping {
			converted[key] = item
		}
		return resolveHeaderOverrideValueByMapping(context, headerName, converted)
	}

	headerValue := strings.TrimSpace(fmt.Sprintf("%v", value))
	if headerValue == "" {
		return "", false, nil
	}
	return headerValue, true, nil
}

func resolveHeaderOverrideValueByMapping(context map[string]interface{}, headerName string, mapping map[string]interface{}) (string, bool, error) {
	if len(mapping) == 0 {
		return "", false, fmt.Errorf("header value mapping cannot be empty")
	}
	if value, exists := mapping["$keep_only_declared"]; exists {
		if _, ok := value.(bool); !ok {
			return "", false, fmt.Errorf("invalid header keep-only option")
		}
	}

	appendTokens, err := parseHeaderAppendTokens(mapping)
	if err != nil {
		return "", false, err
	}
	keepOnlyDeclared := parseHeaderKeepOnlyDeclared(mapping)

	sourceValue, exists := getHeaderValueFromContext(context, headerName)
	sourceTokens := make([]string, 0)
	if exists {
		sourceTokens = splitHeaderListValue(sourceValue)
	}

	wildcardValue, hasWildcard := mapping["*"]
	resultTokens := make([]string, 0, len(sourceTokens)+len(appendTokens))
	for _, token := range sourceTokens {
		replacementRaw, hasReplacement := mapping[token]
		if !hasReplacement && hasWildcard && !keepOnlyDeclared {
			replacementRaw = wildcardValue
			hasReplacement = true
		}
		if !hasReplacement {
			if keepOnlyDeclared {
				continue
			}
			resultTokens = append(resultTokens, token)
			continue
		}
		replacementTokens, err := parseHeaderReplacementTokens(replacementRaw)
		if err != nil {
			return "", false, err
		}
		resultTokens = append(resultTokens, replacementTokens...)
	}

	resultTokens = append(resultTokens, appendTokens...)
	resultTokens = uniqueOverrideStrings(resultTokens)
	if len(resultTokens) == 0 {
		return "", false, nil
	}
	return strings.Join(resultTokens, ","), true, nil
}

func parseHeaderAppendTokens(mapping map[string]interface{}) ([]string, error) {
	appendRaw, ok := mapping["$append"]
	if !ok {
		return nil, nil
	}
	return parseHeaderReplacementTokens(appendRaw)
}

func parseHeaderKeepOnlyDeclared(mapping map[string]interface{}) bool {
	keepOnlyDeclaredRaw, ok := mapping["$keep_only_declared"]
	if !ok {
		return false
	}
	keepOnlyDeclared, ok := keepOnlyDeclaredRaw.(bool)
	if !ok {
		return false
	}
	return keepOnlyDeclared
}

func copyHeaderInContext(context map[string]interface{}, fromHeader, toHeader string, keepOrigin bool) error {
	fromHeader = normalizeHeaderContextKey(fromHeader)
	toHeader = normalizeHeaderContextKey(toHeader)
	if !validOverrideHeaderName(fromHeader) || !validOverrideHeaderName(toHeader) {
		return fmt.Errorf("copy_header from/to is required")
	}
	value, exists := getHeaderValueFromContext(context, fromHeader)
	if !exists {
		return fmt.Errorf("%w: %s", errSourceHeaderNotFound, fromHeader)
	}
	return setHeaderOverrideInContext(context, toHeader, value, keepOrigin)
}

func moveHeaderInContext(context map[string]interface{}, fromHeader, toHeader string, keepOrigin bool) error {
	fromHeader = normalizeHeaderContextKey(fromHeader)
	toHeader = normalizeHeaderContextKey(toHeader)
	if !validOverrideHeaderName(fromHeader) || !validOverrideHeaderName(toHeader) {
		return fmt.Errorf("move_header from/to is required")
	}
	if err := copyHeaderInContext(context, fromHeader, toHeader, keepOrigin); err != nil {
		return err
	}
	if strings.EqualFold(fromHeader, toHeader) {
		return nil
	}
	return deleteHeaderOverrideInContext(context, fromHeader)
}

func deleteHeaderOverrideInContext(context map[string]interface{}, headerName string) error {
	headerName = normalizeHeaderContextKey(headerName)
	if !validOverrideHeaderName(headerName) {
		return fmt.Errorf("header name is required")
	}
	rawHeaders := ensureMapKeyInContext(context, paramOverrideContextHeaderOverride)
	delete(rawHeaders, headerName)
	ensureMapKeyInContext(context, paramOverrideDeletedHeaders)[headerName] = true
	return nil
}

// applyHeaderContextOperation executes a header-mutating ParamOperation
// against the condition context and re-marshals the context JSON on success.
// terminal reports whether err must be returned to the caller as-is instead
// of being wrapped with "operation %s failed: %w" by applyOperations.
func applyHeaderContextOperation(mode string, context map[string]interface{}, op ParamOperation, contextJSON string) (newContextJSON string, err error, terminal bool) {
	switch mode {
	case "set_header":
		err = setHeaderOverrideInContext(context, op.Path, op.Value, op.KeepOrigin)
	case "delete_header":
		err = deleteHeaderOverrideInContext(context, op.Path)
	case "copy_header":
		sourceHeader, targetHeader := headerOperationEndpoints(op)
		err = copyHeaderInContext(context, sourceHeader, targetHeader, op.KeepOrigin)
		if errors.Is(err, errSourceHeaderNotFound) {
			err = nil
		}
	case "move_header":
		sourceHeader, targetHeader := headerOperationEndpoints(op)
		err = moveHeaderInContext(context, sourceHeader, targetHeader, op.KeepOrigin)
		if errors.Is(err, errSourceHeaderNotFound) {
			err = nil
		}
	case "pass_headers":
		var headerNames []string
		headerNames, err = parseHeaderPassThroughNames(op.Value)
		if err != nil {
			return contextJSON, err, true
		}
		for _, headerName := range headerNames {
			if err = copyHeaderInContext(context, headerName, headerName, op.KeepOrigin); err != nil {
				if errors.Is(err, errSourceHeaderNotFound) {
					err = nil
					continue
				}
				break
			}
		}
	default:
		return contextJSON, fmt.Errorf("unknown operation: %s", mode), true
	}
	if err != nil {
		return contextJSON, err, false
	}
	newContextJSON, err = marshalContextJSON(context)
	return newContextJSON, err, false
}

// headerOperationEndpoints resolves the source/target header names for
// copy_header and move_header, falling back to op.Path when From/To are
// unset so a single header name can be given once.
func headerOperationEndpoints(op ParamOperation) (sourceHeader, targetHeader string) {
	sourceHeader = strings.TrimSpace(op.From)
	targetHeader = strings.TrimSpace(op.To)
	if sourceHeader == "" {
		sourceHeader = strings.TrimSpace(op.Path)
	}
	if targetHeader == "" {
		targetHeader = strings.TrimSpace(op.Path)
	}
	return sourceHeader, targetHeader
}

func ensureMapKeyInContext(context map[string]interface{}, key string) map[string]interface{} {
	if context == nil {
		return map[string]interface{}{}
	}
	if existing, ok := context[key]; ok {
		if mapVal, ok := existing.(map[string]interface{}); ok {
			return mapVal
		}
	}
	result := make(map[string]interface{})
	context[key] = result
	return result
}

func getHeaderValueFromContext(context map[string]interface{}, headerName string) (string, bool) {
	headerName = normalizeHeaderContextKey(headerName)
	if headerName == "" {
		return "", false
	}
	if ensureMapKeyInContext(context, paramOverrideDeletedHeaders)[headerName] == true {
		return "", false
	}
	for _, key := range []string{paramOverrideContextHeaderOverride, paramOverrideContextRequestHeaders} {
		source := ensureMapKeyInContext(context, key)
		raw, ok := source[headerName]
		if !ok {
			continue
		}
		value := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if value != "" {
			return value, true
		}
	}
	return "", false
}

func normalizeHeaderContextKey(key string) string {
	return strings.TrimSpace(strings.ToLower(key))
}
