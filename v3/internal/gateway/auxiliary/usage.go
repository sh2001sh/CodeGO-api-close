package auxiliary

import (
	"encoding/binary"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func parseUsage(body []byte) *gateway.Usage {
	root := gjson.ParseBytes(body)
	u := root.Get("usage")
	if !u.Exists() {
		u = root.Get("response.usage")
	}
	if !u.IsObject() {
		if metadata := root.Get("usageMetadata"); metadata.IsObject() {
			return parseGeminiUsageMetadata(metadata)
		}
		return nil
	}
	out := parseCompatibleUsage(u)
	if out == nil {
		return nil
	}
	countResponseToolCalls(out, root)
	return out
}

// parseGeminiUsageMetadata converts a native Gemini usageMetadata object into
// the shared Usage shape, splitting per-modality token counts.
func parseGeminiUsageMetadata(metadata gjson.Result) *gateway.Usage {
	out := &gateway.Usage{PromptTokens: metadata.Get("promptTokenCount").Int() + metadata.Get("toolUsePromptTokenCount").Int(), CompletionTokens: metadata.Get("candidatesTokenCount").Int() + metadata.Get("thoughtsTokenCount").Int(), CachedTokens: metadata.Get("cachedContentTokenCount").Int()}
	for _, item := range metadata.Get("promptTokensDetails").Array() {
		switch item.Get("modality").String() {
		case "IMAGE":
			out.ImageInputTokens += item.Get("tokenCount").Int()
		case "AUDIO":
			out.AudioInputTokens += item.Get("tokenCount").Int()
		}
	}
	for _, item := range metadata.Get("candidatesTokensDetails").Array() {
		switch item.Get("modality").String() {
		case "IMAGE":
			out.ImageOutputTokens += item.Get("tokenCount").Int()
		case "AUDIO":
			out.AudioOutputTokens += item.Get("tokenCount").Int()
		}
	}
	return out
}

// parseCompatibleUsage converts an OpenAI/Anthropic-compatible "usage"
// object into the shared Usage shape, or nil if it carries no recognizable
// usage fields.
func parseCompatibleUsage(u gjson.Result) *gateway.Usage {
	if !u.IsObject() {
		return nil
	}
	input, output := u.Get("prompt_tokens"), u.Get("completion_tokens")
	if !input.Exists() {
		input = u.Get("input_tokens")
	}
	if !output.Exists() {
		output = u.Get("output_tokens")
	}
	if !input.Exists() && !output.Exists() && !u.Get("total_tokens").Exists() && !u.Get("image_count").Exists() && !u.Get("audio_characters").Exists() && !u.Get("audio_duration_micros").Exists() && !u.Get("video_duration_micros").Exists() {
		return nil
	}
	out := &gateway.Usage{PromptTokens: input.Int(), CompletionTokens: output.Int(),
		Estimated: u.Get("estimated").Bool(), ImageCount: u.Get("image_count").Int(), AudioCharacters: u.Get("audio_characters").Int(),
		AudioDurationMicros: u.Get("audio_duration_micros").Int(), VideoDurationMicros: u.Get("video_duration_micros").Int(),
		CachedTokens:      max(u.Get("prompt_tokens_details.cached_tokens").Int(), u.Get("input_tokens_details.cached_tokens").Int()),
		CacheWriteTokens:  u.Get("cache_creation_input_tokens").Int(),
		ImageInputTokens:  max(u.Get("input_tokens_details.image_tokens").Int(), u.Get("prompt_tokens_details.image_tokens").Int()),
		ImageOutputTokens: max(u.Get("output_tokens_details.image_tokens").Int(), u.Get("completion_tokens_details.image_tokens").Int()),
		AudioInputTokens:  max(u.Get("input_tokens_details.audio_tokens").Int(), u.Get("prompt_tokens_details.audio_tokens").Int()),
		AudioOutputTokens: max(u.Get("output_tokens_details.audio_tokens").Int(), u.Get("completion_tokens_details.audio_tokens").Int())}
	if !input.Exists() && !output.Exists() {
		out.PromptTokens = u.Get("total_tokens").Int()
	}
	return out
}

// countResponseToolCalls tallies Responses-API tool invocations (web search,
// file search) from the top-level "output" array into out.ToolCalls.
func countResponseToolCalls(out *gateway.Usage, root gjson.Result) {
	for _, item := range root.Get("output").Array() {
		tool := ""
		switch item.Get("type").String() {
		case "web_search_call":
			tool = "web_search"
		case "file_search_call":
			tool = "file_search"
		}
		if tool != "" {
			if out.ToolCalls == nil {
				out.ToolCalls = make(map[string]int64)
			}
			out.ToolCalls[tool]++
		}
	}
}

func estimate(req *gateway.Request, in Input, output []byte) gateway.Usage {
	u := gateway.Usage{PromptTokens: int64(len(req.Body)+3) / 4, Estimated: true}
	switch in.Operation {
	case Images, ImageEdits, GeminiImages:
		count := int64(len(gjson.GetBytes(output, "data").Array()))
		if count == 0 {
			count = max(gjson.GetBytes(req.Body, "n").Int(), 1)
		}
		// v2 non-token image adapters use the established 258-token unit.
		u.PromptTokens = 258 * count
	case Speech:
		text := gjson.GetBytes(req.Body, "input").String()
		u.PromptTokens = int64(utf8.RuneCountInString(text))
		duration := audioDuration(output, gjson.GetBytes(req.Body, "response_format").String())
		if duration > 0 {
			u.AudioOutputTokens = int64(math.Round(math.Ceil(duration) / 60 * 1000))
		} else {
			u.AudioOutputTokens = int64(len(output)+999) / 1000
		}
		u.CompletionTokens = u.AudioOutputTokens
	case Transcriptions, Translations:
		text := gjson.GetBytes(output, "text").String()
		if text == "" && !gjson.ValidBytes(output) {
			text = string(output)
		}
		u.CompletionTokens = int64(len(text)+3) / 4
		if seconds := gjson.GetBytes(output, "duration").Float(); seconds > 0 {
			u.AudioInputTokens = int64(math.Round(math.Ceil(seconds) / 60 * 1000))
		} else {
			u.AudioInputTokens = (gjson.GetBytes(req.Body, "file_bytes").Int() + 999) / 1000
		}
		u.PromptTokens = u.AudioInputTokens
	case Embeddings, Rerank, Moderations, GeminiEmbed, GeminiBatchEmbed:
		u.CompletionTokens = 0
	case Completions, Edits:
		u.CompletionTokens = (streamTextBytes(gjson.ParseBytes(output)) + 3) / 4
	default:
		u.CompletionTokens = int64(len(output)+3) / 4
	}
	return u
}

// PCM/WAV duration is exact; compressed audio falls back to marked estimation.
func audioDuration(data []byte, format string) float64 {
	if format == "pcm" {
		return float64(len(data)) / 48000
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return compressedDuration(data)
	}
	var rate uint32
	var size int
	for offset := 12; offset+8 <= len(data); {
		length := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start := offset + 8
		if length < 0 || length > len(data)-start {
			return 0
		}
		switch strings.TrimSpace(string(data[offset : offset+4])) {
		case "fmt":
			if length >= 16 {
				format := binary.LittleEndian.Uint16(data[start : start+2])
				channels := binary.LittleEndian.Uint16(data[start+2 : start+4])
				sampleRate := binary.LittleEndian.Uint32(data[start+4 : start+8])
				bits := binary.LittleEndian.Uint16(data[start+14 : start+16])
				if (format != 1 && format != 3) || channels == 0 || channels > 32 || sampleRate == 0 || sampleRate > 384000 || bits == 0 || bits > 64 || bits%8 != 0 {
					return 0
				}
				rate = sampleRate * uint32(channels) * uint32(bits/8)
			}
		case "data":
			size += length
		}
		offset = start + length + (length % 2)
	}
	if rate == 0 {
		return 0
	}
	return float64(size) / float64(rate)
}
