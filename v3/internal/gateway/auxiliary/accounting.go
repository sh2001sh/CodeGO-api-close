package auxiliary

import (
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

// Non-token units remain separate from genuine upstream token accounting.
func enrichUsage(usage *gateway.Usage, req *gateway.Request, in Input, response Response) {
	switch in.Operation {
	case Images, ImageEdits, GeminiImages:
		if count, err := strconv.ParseInt(response.Header.Get("X-Codego-Image-Count"), 10, 64); err == nil && count > 0 {
			usage.ImageCount = count
		}
		if usage.ImageCount == 0 {
			for _, item := range gjson.GetBytes(response.Body, "data").Array() {
				if item.Get("url").String() != "" || item.Get("b64_json").String() != "" {
					usage.ImageCount++
				}
			}
		}
		if usage.ImageCount == 0 {
			for _, item := range gjson.GetBytes(response.Body, "predictions").Array() {
				if item.Get("raiFilteredReason").String() == "" && item.Get("bytesBase64Encoded").String() != "" {
					usage.ImageCount++
				}
			}
		}
	case Speech:
		usage.AudioCharacters = int64(utf8.RuneCountInString(gjson.GetBytes(req.Body, "input").String()))
		if characters, err := strconv.ParseInt(response.Header.Get("X-Codego-Audio-Characters"), 10, 64); err == nil && characters >= 0 {
			usage.AudioCharacters = characters
		}
		if millis, err := strconv.ParseInt(response.Header.Get("X-Codego-Audio-Duration-Milliseconds"), 10, 64); err == nil && millis > 0 && millis <= math.MaxInt64/1000 {
			usage.AudioDurationMicros = millis * 1000
		}
		if duration := audioDuration(response.Body, gjson.GetBytes(req.Body, "response_format").String()); duration > 0 {
			usage.AudioDurationMicros = int64(duration * 1_000_000)
		}
	case Transcriptions, Translations:
		if duration := gjson.GetBytes(response.Body, "duration").Float(); duration > 0 {
			usage.AudioDurationMicros = int64(duration * 1_000_000)
		}
		if usage.AudioDurationMicros == 0 {
			usage.AudioDurationMicros = gjson.GetBytes(req.Body, "audio_duration_micros").Int()
		}
	}
}
