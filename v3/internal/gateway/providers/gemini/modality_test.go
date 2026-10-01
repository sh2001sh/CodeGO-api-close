package gemini

import "testing"

func TestModalityUsageFeedsPricing(t *testing.T) {
	u := usageFrom(&usageMetadata{Prompt: 120, Output: 60, PromptDetails: []modalityTokens{{"AUDIO", 80}, {"IMAGE", 20}}, OutputDetails: []modalityTokens{{"AUDIO", 40}, {"IMAGE", 10}}})
	if u.AudioInputTokens != 80 || u.AudioOutputTokens != 40 || u.ImageInputTokens != 20 || u.ImageOutputTokens != 10 {
		t.Fatalf("usage=%+v", u)
	}
}
