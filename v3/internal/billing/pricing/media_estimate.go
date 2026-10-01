package pricing

import (
	"fmt"
	"unicode/utf8"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

// EstimateUsageForPrice estimates admission against the actual configured
// dimension. JSON billing views must provide a measurable audio/video duration;
// bytes and token guesses are not a substitute for duration. Missing image n
// retains the API's default of one; an explicit zero or negative n is invalid.
func EstimateUsageForPrice(body []byte, p catalog.Price, cfg EstimateConfig) (gateway.Usage, error) {
	unit, err := MediaUnit(p)
	if err != nil {
		return gateway.Usage{}, err
	}
	if unit == "" {
		return EstimateUsage(body, cfg), nil
	}
	if !gjson.ValidBytes(body) {
		return gateway.Usage{}, fmt.Errorf("pricing: invalid media billing JSON")
	}
	usage := gateway.Usage{Estimated: true}
	if unit == "video_second" {
		duration, _, err := frozenVideoFacts(p)
		if err != nil {
			return usage, err
		}
		if duration > 0 {
			usage.VideoDurationMicros = duration
			return usage, nil
		}
	}
	switch unit {
	case "image":
		return estimateImageUsage(body, usage, unit)
	case "audio_character":
		return estimateAudioCharacterUsage(body, usage)
	case "audio_second", "video_second":
		return estimateDurationUsage(body, usage, unit)
	}
	return usage, fmt.Errorf("pricing: unknown media unit %q", unit)
}

// estimateImageUsage reads the image count dimension, defaulting to the
// API's implicit n=1 when the request doesn't specify one.
func estimateImageUsage(body []byte, usage gateway.Usage, unit string) (gateway.Usage, error) {
	count := firstDimension(body, "image_count", "n", "parameters.n", "num_images", "sampleCount", "parameters.sampleCount")
	if !count.Exists() {
		usage.ImageCount = 1
		return usage, nil
	}
	return WithMediaUnits(usage, unit, dimensionText(count))
}

// estimateAudioCharacterUsage counts the TTS input text's runes.
func estimateAudioCharacterUsage(body []byte, usage gateway.Usage) (gateway.Usage, error) {
	text := gjson.GetBytes(body, "input")
	if !text.Exists() {
		text = gjson.GetBytes(body, "text")
	}
	if text.Type != gjson.String || text.String() == "" || !utf8.ValidString(text.String()) {
		return usage, fmt.Errorf("pricing: missing TTS input text")
	}
	usage.AudioCharacters = int64(utf8.RuneCountInString(text.String()))
	return usage, nil
}

// estimateDurationUsage reads an audio or video duration, preferring an
// exact microsecond field and falling back to a seconds-based dimension.
func estimateDurationUsage(body []byte, usage gateway.Usage, unit string) (gateway.Usage, error) {
	field := "audio_duration_micros"
	if unit == "video_second" {
		field = "video_duration_micros"
	}
	if micros := firstDimension(body, field, "duration_micros"); micros.Exists() {
		count, err := exactNumber(dimensionText(micros))
		if err != nil || count.Sign() <= 0 || !count.IsInt() || !count.Num().IsInt64() {
			return usage, fmt.Errorf("pricing: invalid %s", field)
		}
		if unit == "audio_second" {
			usage.AudioDurationMicros = count.Num().Int64()
		} else {
			usage.VideoDurationMicros = count.Num().Int64()
		}
		return usage, nil
	}
	seconds := firstDimension(body, "duration", "seconds", "duration_seconds", "parameters.duration", "parameters.durationSeconds")
	if !seconds.Exists() {
		return usage, fmt.Errorf("pricing: missing %s estimate", unit)
	}
	return WithMediaUnits(usage, unit, dimensionText(seconds))
}

func firstDimension(body []byte, paths ...string) gjson.Result {
	for _, path := range paths {
		if value := gjson.GetBytes(body, path); value.Exists() {
			return value
		}
	}
	return gjson.Result{}
}

func dimensionText(value gjson.Result) string {
	if value.Type == gjson.String {
		return value.String()
	}
	return value.Raw
}
