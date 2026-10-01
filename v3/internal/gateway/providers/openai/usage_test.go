package openai

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestUsageCarriesModalityPricingInputs(t *testing.T) {
	u := `"usage":{"prompt_tokens":120,"completion_tokens":60,"prompt_tokens_details":{"audio_tokens":80,"image_tokens":20},"completion_tokens_details":{"audio_tokens":40,"image_tokens":10}}`
	for _, streaming := range []bool{false, true} {
		body := `{"choices":[{"message":{"content":"hello"}}],` + u + `}`
		var s gateway.EventStream
		if streaming {
			s = decoder("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[]," + u + "}\n\ndata: [DONE]\n\n")
			_, _ = s.Next()
		} else {
			s = (Provider{}).Decode(&gateway.Request{}, &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))})
		}
		ev, err := s.Next()
		if err != nil || ev.Usage == nil || ev.Usage.AudioInputTokens != 80 || ev.Usage.AudioOutputTokens != 40 || ev.Usage.ImageInputTokens != 20 || ev.Usage.ImageOutputTokens != 10 {
			t.Fatalf("streaming=%v usage=%+v err=%v", streaming, ev.Usage, err)
		}
		_ = s.Close()
	}
}
