package auxiliary

import "encoding/json"

func compatibleSupports(provider string, operation Operation) bool {
	switch provider {
	case "deepseek":
		return operation == Completions
	case "mistral":
		return false // v2 vector/media conversions are unimplemented.
	case "moonshot":
		return operation == Embeddings || operation == Completions
	case "xai":
		return operation == Images || operation == ImageEdits || operation == Compact || operation == Completions
	case "codex":
		return operation == Compact || operation == Search
	case "azure":
		return operation != Search
	default:
		return true
	}
}

func xaiImageBody(body []byte) ([]byte, error) {
	fields, err := object(body)
	if err != nil {
		return nil, err
	}
	result := make(map[string]json.RawMessage)
	for _, name := range []string{"model", "prompt", "n", "response_format"} {
		if value := fields[name]; len(value) > 0 {
			result[name] = value
		}
	}
	if len(result["n"]) == 0 {
		result["n"] = json.RawMessage("1")
	}
	return json.Marshal(result)
}
