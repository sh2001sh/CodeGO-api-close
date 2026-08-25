package contract

import "strings"

var (
	openAIResponseOnlyModels = []string{
		"o3-pro",
		"o3-deep-research",
		"o4-mini-deep-research",
	}
	imageGenerationModelPrefixes = []string{
		"dall-e-",
		"gpt-image-",
		"imagen-",
		"grok-imagine-image",
		"grok-2-image",
	}
	imageGenerationModelFragments = []string{
		"flux-",
		"flux.1-",
		"image-generation",
	}
	openAITextModels = []string{
		"gpt-",
		"o1",
		"o3",
		"o4",
		"chatgpt",
	}
)

// IsOpenAIResponseOnlyModel reports whether a model is only available through the OpenAI Responses API.
func IsOpenAIResponseOnlyModel(modelName string) bool {
	for _, model := range openAIResponseOnlyModels {
		if strings.Contains(modelName, model) {
			return true
		}
	}
	return false
}

// IsImageGenerationModel reports whether a model should use the image-generation endpoint.
func IsImageGenerationModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	for _, prefix := range imageGenerationModelPrefixes {
		if strings.HasPrefix(modelName, prefix) {
			return true
		}
	}
	for _, fragment := range imageGenerationModelFragments {
		if strings.Contains(modelName, fragment) {
			return true
		}
	}
	return false
}

// IsOpenAITextModel reports whether a model should use the OpenAI tokenizer path.
func IsOpenAITextModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	for _, model := range openAITextModels {
		if strings.Contains(modelName, model) {
			return true
		}
	}
	return false
}
