package openai

import "github.com/tidwall/gjson"

// Every requested choice consumes tokens, including hidden reasoning/refusal
// and tool arguments. Count each generated field once when usage is absent.
func generatedBytes(data []byte, field string) int {
	total := 0
	for _, choice := range gjson.GetBytes(data, "choices").Array() {
		message := choice.Get(field)
		for _, name := range []string{"content", "reasoning_content", "reasoning", "refusal"} {
			total += len(message.Get(name).Str)
		}
		for _, call := range message.Get("tool_calls").Array() {
			total += len(call.Get("function.arguments").Str)
		}
		total += len(message.Get("function_call.arguments").Str)
	}
	return total
}
