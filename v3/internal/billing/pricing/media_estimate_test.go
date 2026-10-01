package pricing

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestEstimateMediaUnits(t *testing.T) {
	for _, test := range []struct {
		unit, body string
		want       gateway.Usage
	}{
		{"image", `{}`, gateway.Usage{ImageCount: 1, Estimated: true}},
		{"image", `{"n":4}`, gateway.Usage{ImageCount: 4, Estimated: true}},
		{"image", `{"n":4,"image_count":2}`, gateway.Usage{ImageCount: 2, Estimated: true}},
		{"image", `{"parameters":{"n":3}}`, gateway.Usage{ImageCount: 3, Estimated: true}},
		{"audio_second", `{"duration":"60.25"}`, gateway.Usage{AudioDurationMicros: 60_250_000, Estimated: true}},
		{"audio_second", `{"audio_duration_micros":9007199254740993}`, gateway.Usage{AudioDurationMicros: 9_007_199_254_740_993, Estimated: true}},
		{"video_second", `{"duration":5.000001}`, gateway.Usage{VideoDurationMicros: 5_000_001, Estimated: true}},
		{"video_second", `{"video_duration_micros":"1250000"}`, gateway.Usage{VideoDurationMicros: 1_250_000, Estimated: true}},
		{"audio_character", `{"input":"汉字🙂é"}`, gateway.Usage{AudioCharacters: 4, Estimated: true}},
		{"audio_character", `{"text":"abc"}`, gateway.Usage{AudioCharacters: 3, Estimated: true}},
	} {
		t.Run(test.unit+test.body, func(t *testing.T) {
			got, err := EstimateUsageForPrice([]byte(test.body), mediaPrice(test.unit, 1), EstimateConfig{})
			if err != nil || got.ImageCount != test.want.ImageCount || got.AudioDurationMicros != test.want.AudioDurationMicros ||
				got.AudioCharacters != test.want.AudioCharacters || got.VideoDurationMicros != test.want.VideoDurationMicros ||
				!got.Estimated || got.PromptTokens != 0 || got.CompletionTokens != 0 {
				t.Fatalf("EstimateUsageForPrice = %#v, %v; want %#v", got, err, test.want)
			}
		})
	}
	got, err := EstimateUsageForPrice([]byte(`{"max_tokens":17}`), catalog.Price{Mode: "per_token"}, EstimateConfig{})
	if err != nil || got.CompletionTokens != 17 || got.PromptTokens == 0 {
		t.Fatalf("token estimate changed: %#v, %v", got, err)
	}
}

func TestEstimateMediaRejectsMissingAndInvalidDimensions(t *testing.T) {
	for _, test := range []struct{ unit, body string }{
		{"image", `{`},
		{"image", `{"n":0}`}, {"image", `{"n":-1}`}, {"image", `{"n":1.5}`}, {"image", `{"n":true}`},
		{"audio_second", `{}`}, {"audio_second", `{"file_bytes":1234}`}, {"audio_second", `{"duration":0}`},
		{"audio_second", `{"duration":0.0000001}`}, {"audio_second", `{"audio_duration_micros":-1}`},
		{"video_second", `{"duration":-5}`}, {"video_second", `{"video_duration_micros":1.5}`},
		{"audio_character", `{}`}, {"audio_character", `{"input":""}`}, {"audio_character", `{"input":123}`},
	} {
		if _, err := EstimateUsageForPrice([]byte(test.body), mediaPrice(test.unit, 1), EstimateConfig{}); err == nil {
			t.Fatalf("accepted invalid estimate %#v", test)
		}
	}
}
