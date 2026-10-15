package tokenx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLargePromptEstimateUsesExactCounterBelowThreshold(t *testing.T) {
	text := strings.Repeat("small prompt ", 100)
	require.Equal(t, CountTextToken(text, "gpt-4o"), countPromptTextTokens(text, "gpt-4o"))
}

func TestLargePromptEstimateAddsBoundedSafetyMargin(t *testing.T) {
	tests := map[string]string{
		"english": strings.Repeat("The quick brown fox jumps over the lazy dog. ", 8_000),
		"cjk":     strings.Repeat("这是用于测试长上下文令牌估算的中文内容。", 10_000),
		"code":    strings.Repeat("func calculateValue(input string) int { return len(input) + 42 }\n", 7_000),
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			exact := CountTextToken(text, "gpt-4o")
			estimated := countPromptTextTokens(text, "gpt-4o")
			require.GreaterOrEqual(t, estimated, exact)
			require.LessOrEqual(t, estimated, int(float64(exact)*1.15)+5)
		})
	}
}

func BenchmarkLargePromptTokenCounting(b *testing.B) {
	text := strings.Repeat("The quick brown fox calls calculateValue(input) for a long context.\n", 16_000)
	b.Run("exact", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = CountTextToken(text, "gpt-4o")
		}
	})
	b.Run("sampled", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = countPromptTextTokens(text, "gpt-4o")
		}
	})
}
