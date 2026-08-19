package tokenx

import (
	"math"
	"unicode/utf8"

	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
)

const (
	largePromptSampleThreshold = 256 << 10
	largePromptSampleWindow    = 8 << 10
	largePromptSafetyFactor    = 1.10
)

func countPromptTextTokens(text string, model string) int {
	if len(text) < largePromptSampleThreshold || !gatewaycontract.IsOpenAITextModel(model) {
		return CountTextToken(text, model)
	}
	return estimateLargeOpenAIPromptTokens(text, model)
}

// estimateLargeOpenAIPromptTokens samples five evenly distributed windows with
// the same tokenizer used by exact counting. The safety factor is only for the
// temporary reservation; upstream usage remains authoritative for settlement.
func estimateLargeOpenAIPromptTokens(text string, model string) int {
	if text == "" {
		return 0
	}
	if len(text) < largePromptSampleThreshold {
		return CountTextToken(text, model)
	}

	lastStart := len(text) - largePromptSampleWindow
	starts := [...]int{0, lastStart / 4, lastStart / 2, lastStart * 3 / 4, lastStart}
	encoder := getTokenEncoder(model)
	sampledBytes := 0
	sampledTokens := 0
	for _, start := range starts {
		start = nextRuneBoundary(text, start)
		end := previousRuneBoundary(text, start+largePromptSampleWindow)
		if end <= start {
			continue
		}
		sample := text[start:end]
		sampledBytes += len(sample)
		sampledTokens += getTokenNum(encoder, sample)
	}
	if sampledBytes == 0 {
		return CountTextToken(text, model)
	}
	estimated := float64(sampledTokens) * float64(len(text)) / float64(sampledBytes)
	return int(math.Ceil(estimated * largePromptSafetyFactor))
}

func nextRuneBoundary(text string, offset int) int {
	if offset < 0 {
		return 0
	}
	if offset >= len(text) {
		return len(text)
	}
	for offset < len(text) && !utf8.RuneStart(text[offset]) {
		offset++
	}
	return offset
}

func previousRuneBoundary(text string, offset int) int {
	if offset > len(text) {
		offset = len(text)
	}
	for offset > 0 && offset < len(text) && !utf8.RuneStart(text[offset]) {
		offset--
	}
	return offset
}
