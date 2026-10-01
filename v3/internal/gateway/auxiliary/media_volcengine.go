package auxiliary

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func buildVolcMedia(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	if in.Operation == Images || in.Operation == ImageEdits {
		return buildVolcImageMedia(ctx, req, target, in)
	}
	if in.Operation != Speech {
		return nil, unsupported(in.Operation)
	}
	return buildVolcSpeechMedia(ctx, req, target)
}

// buildVolcImageMedia builds the images/generations request for Volcengine
// Ark, inlining multipart edit images when the client sent a multipart body.
func buildVolcImageMedia(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	base := mediaBase(target, "https://ark.cn-beijing.volces.com")
	payload, err := mediaFields(req.Body)
	if err != nil {
		return nil, err
	}
	payload["model"] = upstreamModel(req, target)
	if in.Operation == ImageEdits && strings.HasPrefix(in.ContentType, "multipart/") {
		images, err := mediaMultipartImages(in)
		if err != nil {
			return nil, err
		}
		payload["image"] = images
		for name := range payload {
			if strings.HasSuffix(name, "_bytes") {
				delete(payload, name)
			}
		}
	}
	address, err := endpoint(base, "/api/v3/images/generations")
	if err != nil {
		return nil, err
	}
	return jsonRequest(ctx, address, target.Secret, payload)
}

// buildVolcSpeechMedia builds the TTS request for Volcengine, choosing
// between the Ark REST endpoint and the legacy ByteDance websocket endpoint
// depending on the channel's configured base URL.
func buildVolcSpeechMedia(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	parts := strings.Split(target.Secret, "|")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, mediaError("volcengine", "invalid_channel_credential")
	}
	voice := gjson.GetBytes(req.Body, "voice").String()
	voices := map[string]string{"alloy": "zh_male_M392_conversation_wvae_bigtts", "echo": "zh_male_wenhao_mars_bigtts", "fable": "zh_female_tianmei_mars_bigtts", "onyx": "zh_male_zhibei_mars_bigtts", "nova": "zh_female_shuangkuaisisi_mars_bigtts", "shimmer": "zh_female_cancan_mars_bigtts"}
	if value := voices[voice]; value != "" {
		voice = value
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	id[6], id[8] = (id[6]&15)|64, (id[8]&63)|128
	requestID := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	payload := map[string]any{
		"app":     map[string]any{"appid": parts[0], "token": parts[1], "cluster": "volcano_tts"},
		"user":    map[string]any{"uid": "openai_relay_user"},
		"audio":   map[string]any{"voice_type": voice, "encoding": volcEncoding(req.Body), "speed_ratio": gjson.GetBytes(req.Body, "speed").Float(), "rate": 24000},
		"request": map[string]any{"reqid": requestID, "text": gjson.GetBytes(req.Body, "input").String(), "operation": "query", "model": upstreamModel(req, target)},
	}
	if err := mergeMediaMetadata(payload, req.Body, "metadata"); err != nil {
		return nil, err
	}
	// Credentials always come from the selected channel, including metadata requests.
	app, ok := payload["app"].(map[string]any)
	if !ok {
		return nil, mediaError("volcengine", "invalid_metadata")
	}
	app["appid"], app["token"] = parts[0], parts[1]
	request, ok := payload["request"].(map[string]any)
	if !ok {
		return nil, mediaError("volcengine", "invalid_metadata")
	}
	request["model"], request["operation"] = upstreamModel(req, target), "query"
	base := mediaBase(target, "https://ark.cn-beijing.volces.com")
	path := "/v1/audio/speech"
	if target.BaseURL == "" || strings.TrimRight(target.BaseURL, "/") == "https://ark.cn-beijing.volces.com" {
		base, path = "https://openspeech.bytedance.com", "/api/v1/tts/ws_binary"
		request["operation"] = "submit"
	}
	address, err := endpoint(base, path)
	if err != nil {
		return nil, err
	}
	r, err := jsonRequest(ctx, address, "", payload)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer;"+parts[1])
	if path == "/api/v1/tts/ws_binary" {
		r.URL.Scheme = "wss"
	}
	return r, nil
}

func volcEncoding(body []byte) string {
	if encoding := gjson.GetBytes(body, "metadata.audio.encoding").String(); encoding != "" {
		return encoding
	}
	format := gjson.GetBytes(body, "response_format").String()
	switch format {
	case "opus":
		return "ogg_opus"
	case "wav", "pcm":
		return format
	default:
		return "mp3"
	}
}

func decodeVolcMedia(req *gateway.Request, in Input, data []byte) (Response, error) {
	if in.Operation == Speech {
		if gjson.GetBytes(data, "code").Int() != 3000 {
			return Response{}, mediaError("volcengine", "volcengine_tts_error")
		}
		audio, err := base64.StdEncoding.DecodeString(gjson.GetBytes(data, "data").String())
		if err != nil {
			return Response{}, mediaError("volcengine", "invalid_audio")
		}
		usage := parseUsage(data)
		text := gjson.GetBytes(req.Body, "input").String()
		if value := gjson.GetBytes(req.Body, "metadata.request.text"); value.Exists() {
			text = value.String()
		}
		characters := utf8.RuneCountInString(text)
		if usage == nil {
			usage = &gateway.Usage{PromptTokens: int64(characters), Estimated: true}
		}
		response, err := speechMediaResponse(audio, volcEncoding(req.Body), usage)
		if err != nil {
			return Response{}, err
		}
		response.Header.Set("X-Codego-Audio-Characters", strconv.Itoa(characters))
		if length := gjson.GetBytes(data, "addition.duration").Int(); length > 0 {
			response.Header.Set("X-Codego-Audio-Duration-Milliseconds", strconv.FormatInt(length, 10))
		}
		return response, nil
	}
	if gjson.GetBytes(data, "error").Exists() {
		return Response{}, mediaError("volcengine", "volcengine_image_error")
	}
	var images []mediaImage
	for _, image := range gjson.GetBytes(data, "data").Array() {
		images = append(images, mediaImage{URL: image.Get("url").String(), Base64: image.Get("b64_json").String(), RevisedPrompt: image.Get("revised_prompt").String()})
	}
	return imageMediaResponse(req, images, parseUsage(data), nil)
}
