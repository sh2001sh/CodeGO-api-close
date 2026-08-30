package execution

import (
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/dto"
	platformcopy "github.com/sh2001sh/new-api/internal/platform/copyx"
)

func benchmarkOpenAIRequest() *dto.GeneralOpenAIRequest {
	content := strings.Repeat("context ", 1<<15)
	return &dto.GeneralOpenAIRequest{
		Model: "gpt-4.1",
		Messages: []dto.Message{{
			Role:    "user",
			Content: content,
		}},
	}
}

func BenchmarkRequestCopyAB(b *testing.B) {
	original := benchmarkOpenAIRequest()
	b.Run("A_full_deep_copy", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			copy, err := platformcopy.DeepCopy(original)
			if err != nil {
				b.Fatal(err)
			}
			if copy.Model == "" {
				b.Fatal("copy lost model")
			}
		}
	})
	b.Run("B_deferred_shallow_copy", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			copy := *original
			if copy.Model == "" {
				b.Fatal("copy lost model")
			}
		}
	})
}
