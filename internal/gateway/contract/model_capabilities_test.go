package contract

import "testing"

func TestIsOpenAIResponseOnlyModel(t *testing.T) {
	t.Parallel()

	if !IsOpenAIResponseOnlyModel("o3-pro") {
		t.Fatal("expected o3-pro to require responses API")
	}
	if IsOpenAIResponseOnlyModel("gpt-4o-mini") {
		t.Fatal("did not expect gpt-4o-mini to require responses API")
	}
}

func TestIsImageGenerationModel(t *testing.T) {
	t.Parallel()

	imageModels := []string{
		"gpt-image-1",
		"gpt-image-2-1k",
		"imagen-3.0-generate",
		"black-forest-labs/flux-1.1-pro",
		"grok-imagine-image",
		"grok-imagine-image-2.0",
		"grok-imagine-image-quality-lite",
		"grok-2-image-1212",
	}
	for _, model := range imageModels {
		if !IsImageGenerationModel(model) {
			t.Fatalf("expected %s to be treated as image generation", model)
		}
	}

	textModels := []string{"gpt-4o-mini", "grok-2-vision-1212", "grok-4.5"}
	for _, model := range textModels {
		if IsImageGenerationModel(model) {
			t.Fatalf("did not expect %s to be treated as image generation", model)
		}
	}
}

func TestIsOpenAITextModel(t *testing.T) {
	t.Parallel()

	if !IsOpenAITextModel("gpt-4o-mini") {
		t.Fatal("expected gpt-4o-mini to use OpenAI text tokenizer")
	}
	if !IsOpenAITextModel("ChatGPT-4o") {
		t.Fatal("expected chatgpt family to use OpenAI text tokenizer")
	}
	if IsOpenAITextModel("claude-3-5-sonnet") {
		t.Fatal("did not expect claude to use OpenAI text tokenizer")
	}
}
