package auxiliary

import "github.com/tidwall/gjson"

// Count generated content, excluding IDs, usage and empty finish frames.
func streamTextBytes(root gjson.Result) int64 {
	var count int64
	for _, choice := range root.Get("choices").Array() {
		count += int64(len(choice.Get("text").String()))
		content := choice.Get("delta.content")
		if content.Type == gjson.String {
			count += int64(len(content.String()))
		} else {
			for _, part := range content.Array() {
				count += int64(len(part.Get("text").String()))
			}
		}
	}
	if root.Get("type").String() == "response.output_text.delta" {
		count += int64(len(root.Get("delta").String()))
	}
	return count
}

func streamHasOutput(root gjson.Result) bool {
	if streamTextBytes(root) > 0 {
		return true
	}
	for _, choice := range root.Get("choices").Array() {
		if choice.Get("delta.function_call.arguments").String() != "" {
			return true
		}
		for _, call := range choice.Get("delta.tool_calls").Array() {
			if call.Get("function.arguments").String() != "" {
				return true
			}
		}
	}
	return false
}
