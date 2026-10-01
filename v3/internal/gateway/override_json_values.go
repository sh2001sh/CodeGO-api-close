package gateway

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func modifyValue(data []byte, path string, value interface{}, keepOrigin, isPrepend bool) ([]byte, error) {
	current := gjson.GetBytes(data, path)
	switch {
	case current.IsArray():
		return modifyArray(data, path, value, isPrepend)
	case current.Type == gjson.String:
		return modifyString(data, path, value, isPrepend)
	case current.Type == gjson.JSON:
		return mergeObjects(data, path, value, keepOrigin)
	}
	return data, fmt.Errorf("operation not supported for type: %v", current.Type)
}

func modifyArray(data []byte, path string, value interface{}, isPrepend bool) ([]byte, error) {
	current := gjson.GetBytes(data, path)
	var newArray []interface{}
	addValue := func() {
		if arr, ok := value.([]interface{}); ok {
			newArray = append(newArray, arr...)
		} else {
			newArray = append(newArray, value)
		}
	}
	addOriginal := func() {
		current.ForEach(func(_, val gjson.Result) bool {
			newArray = append(newArray, val.Value())
			return true
		})
	}
	if isPrepend {
		addValue()
		addOriginal()
	} else {
		addOriginal()
		addValue()
	}
	return sjson.SetBytes(data, path, newArray)
}

func modifyString(data []byte, path string, value interface{}, isPrepend bool) ([]byte, error) {
	current := gjson.GetBytes(data, path)
	valueStr := fmt.Sprintf("%v", value)
	var newStr string
	if isPrepend {
		newStr = valueStr + current.String()
	} else {
		newStr = current.String() + valueStr
	}
	return sjson.SetBytes(data, path, newStr)
}

func trimStringValue(data []byte, path string, value interface{}, isPrefix bool) ([]byte, error) {
	current := gjson.GetBytes(data, path)
	if current.Type != gjson.String {
		return data, fmt.Errorf("operation not supported for type: %v", current.Type)
	}

	if value == nil {
		return data, fmt.Errorf("trim value is required")
	}
	valueStr := fmt.Sprintf("%v", value)

	var newStr string
	if isPrefix {
		newStr = strings.TrimPrefix(current.String(), valueStr)
	} else {
		newStr = strings.TrimSuffix(current.String(), valueStr)
	}
	return sjson.SetBytes(data, path, newStr)
}

func ensureStringAffix(data []byte, path string, value interface{}, isPrefix bool) ([]byte, error) {
	current := gjson.GetBytes(data, path)
	if current.Type != gjson.String {
		return data, fmt.Errorf("operation not supported for type: %v", current.Type)
	}

	if value == nil {
		return data, fmt.Errorf("ensure value is required")
	}
	valueStr := fmt.Sprintf("%v", value)
	if valueStr == "" {
		return data, fmt.Errorf("ensure value is required")
	}

	currentStr := current.String()
	if isPrefix {
		if strings.HasPrefix(currentStr, valueStr) {
			return data, nil
		}
		return sjson.SetBytes(data, path, valueStr+currentStr)
	}

	if strings.HasSuffix(currentStr, valueStr) {
		return data, nil
	}
	return sjson.SetBytes(data, path, currentStr+valueStr)
}

func transformStringValue(data []byte, path string, transform func(string) string) ([]byte, error) {
	current := gjson.GetBytes(data, path)
	if current.Type != gjson.String {
		return data, fmt.Errorf("operation not supported for type: %v", current.Type)
	}
	return sjson.SetBytes(data, path, transform(current.String()))
}

func replaceStringValue(data []byte, path, from, to string) ([]byte, error) {
	current := gjson.GetBytes(data, path)
	if current.Type != gjson.String {
		return data, fmt.Errorf("operation not supported for type: %v", current.Type)
	}
	if from == "" {
		return data, fmt.Errorf("replace from is required")
	}
	return sjson.SetBytes(data, path, strings.ReplaceAll(current.String(), from, to))
}

func regexReplaceStringValue(data []byte, path, pattern, replacement string) ([]byte, error) {
	current := gjson.GetBytes(data, path)
	if current.Type != gjson.String {
		return data, fmt.Errorf("operation not supported for type: %v", current.Type)
	}
	if pattern == "" {
		return data, fmt.Errorf("regex pattern is required")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return data, err
	}
	return sjson.SetBytes(data, path, re.ReplaceAllString(current.String(), replacement))
}

func mergeObjects(data []byte, path string, value interface{}, keepOrigin bool) ([]byte, error) {
	current := gjson.GetBytes(data, path)
	var currentMap, newMap map[string]interface{}
	if err := unmarshalOverrideString(current.Raw, &currentMap); err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case map[string]interface{}:
		newMap = v
	default:
		jsonBytes, marshalErr := json.Marshal(v)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if err := json.Unmarshal(jsonBytes, &newMap); err != nil {
			return nil, err
		}
	}
	result := make(map[string]interface{})
	for k, v := range currentMap {
		result[k] = v
	}
	for k, v := range newMap {
		if !keepOrigin || result[k] == nil {
			result[k] = v
		}
	}
	return sjson.SetBytes(data, path, result)
}
