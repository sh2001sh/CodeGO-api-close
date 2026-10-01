package auxiliary

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func buildMiniMaxMedia(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	body := req.Body
	model := upstreamModel(req, target)
	var path string
	var payload map[string]any
	switch in.Operation {
	case Images:
		path = "/v1/image_generation"
		format := gjson.GetBytes(body, "response_format").String()
		switch format {
		case "b64_json", "base64":
			format = "base64"
		case "":
			format = "url"
		}
		payload = map[string]any{"model": model, "prompt": gjson.GetBytes(body, "prompt").String(), "n": max(gjson.GetBytes(body, "n").Int(), 1), "response_format": format}
		if ratio := miniMaxAspectRatio(body); ratio != "" {
			payload["aspect_ratio"] = ratio
		}
		for _, pair := range [][2]string{{"watermark", "aigc_watermark"}, {"prompt_optimizer", "prompt_optimizer"}} {
			if v := gjson.GetBytes(body, pair[0]); v.Exists() {
				payload[pair[1]] = v.Bool()
			}
		}
	case Speech:
		path = "/v1/t2a_v2"
		format := gjson.GetBytes(body, "response_format").String()
		if format == "" {
			format = "mp3"
		}
		voice := map[string]any{"voice_id": gjson.GetBytes(body, "voice").String()}
		if speed := gjson.GetBytes(body, "speed"); speed.Exists() {
			voice["speed"] = speed.Float()
		}
		payload = map[string]any{"model": model, "text": gjson.GetBytes(body, "input").String(), "voice_setting": voice, "audio_setting": map[string]any{"format": format}, "output_format": "hex"}
		if err := mergeMediaMetadata(payload, body, "metadata"); err != nil {
			return nil, err
		}
		payload["model"] = model
		// This adapter returns complete binary audio, so native streaming is disabled.
		payload["stream"] = false
	default:
		return nil, unsupported(in.Operation)
	}
	base := mediaBase(target, "https://api.minimax.chat")
	if strings.HasSuffix(base, "/v1") {
		path = strings.TrimPrefix(path, "/v1")
	}
	address, err := endpoint(base, path)
	if err != nil {
		return nil, err
	}
	return jsonRequest(ctx, address, target.Secret, payload)
}

func miniMaxAspectRatio(body []byte) string {
	if ratio := gjson.GetBytes(body, "aspect_ratio").String(); ratio != "" {
		return ratio
	}
	size := gjson.GetBytes(body, "size").String()
	standard := map[string]string{"1024x1024": "1:1", "1792x1024": "16:9", "1024x1792": "9:16", "1536x1024": "3:2", "1248x832": "3:2", "1024x1536": "2:3", "832x1248": "2:3", "1152x864": "4:3", "864x1152": "3:4", "1344x576": "21:9"}
	if ratio := standard[size]; ratio != "" {
		return ratio
	}
	w, h, ok := strings.Cut(size, "x")
	if !ok {
		return ""
	}
	width, e1 := strconv.Atoi(w)
	height, e2 := strconv.Atoi(h)
	if e1 != nil || e2 != nil || width <= 0 || height <= 0 {
		return ""
	}
	a, b := width, height
	for b != 0 {
		a, b = b, a%b
	}
	ratio := strconv.Itoa(width/a) + ":" + strconv.Itoa(height/a)
	for _, accepted := range []string{"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "21:9"} {
		if accepted == ratio {
			return ratio
		}
	}
	return ""
}

func (a *mediaAdapter) decodeMiniMax(ctx context.Context, req *gateway.Request, target gateway.Target, in Input, data []byte) (Response, error) {
	if gjson.GetBytes(data, "base_resp.status_code").Int() != 0 {
		return Response{}, mediaError(a.provider, "minimax_media_error")
	}
	if in.Operation == Speech {
		value := gjson.GetBytes(data, "data.audio").String()
		var audio []byte
		var err error
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			audio, err = a.fetch(ctx, target, value)
		} else {
			audio, err = hex.DecodeString(value)
		}
		if err != nil {
			if ctx.Err() != nil {
				return Response{}, ctx.Err()
			}
			return Response{}, mediaError(a.provider, "invalid_audio")
		}
		characters := gjson.GetBytes(data, "extra_info.usage_characters")
		if characters.Int() < 0 {
			return Response{}, mediaError(a.provider, "invalid_usage")
		}
		usage := &gateway.Usage{PromptTokens: characters.Int(), Estimated: !characters.Exists()}
		if usage.Estimated {
			text := gjson.GetBytes(req.Body, "input").String()
			if value := gjson.GetBytes(req.Body, "metadata.text"); value.Exists() {
				text = value.String()
			}
			usage.PromptTokens = int64(utf8.RuneCountInString(text))
		}
		format := gjson.GetBytes(req.Body, "response_format").String()
		if value := gjson.GetBytes(req.Body, "metadata.audio_setting.format").String(); value != "" {
			format = value
		}
		response, err := speechMediaResponse(audio, format, usage)
		if err != nil {
			return Response{}, err
		}
		response.Header.Set("X-Codego-Audio-Characters", strconv.FormatInt(usage.PromptTokens, 10))
		if length := gjson.GetBytes(data, "extra_info.audio_length").Int(); length > 0 {
			response.Header.Set("X-Codego-Audio-Duration-Milliseconds", strconv.FormatInt(length, 10))
		}
		return response, nil
	}
	var images []mediaImage
	for _, value := range gjson.GetBytes(data, "data.image_urls").Array() {
		images = append(images, mediaImage{URL: value.String()})
	}
	for _, value := range gjson.GetBytes(data, "data.image_base64").Array() {
		images = append(images, mediaImage{Base64: value.String()})
	}
	return imageMediaResponse(req, images, parseUsage(data), gjson.GetBytes(data, "metadata").Value())
}
