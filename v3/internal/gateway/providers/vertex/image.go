package vertex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func invalidImage(message string) error {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "invalid_image_request", Message: message}
}

func buildImagenRequest(ctx context.Context, req *gateway.Request) (*http.Request, error) {
	if req.Stream {
		return nil, invalidImage("Imagen does not support streaming")
	}
	body := req.Body
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, invalidImage("Imagen request must be a JSON object")
	}
	if req.Protocol == gateway.ProtocolOpenAIChat {
		converted, err := convertImagenChatBody(body)
		if err != nil {
			return nil, err
		}
		body = converted
	}
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://vertex.invalid", bytes.NewReader(body))
	if err == nil {
		out.Header.Set("Content-Type", "application/json")
	}
	return out, err
}

// convertImagenChatBody converts an OpenAI-shaped image generation request
// into Imagen's native instances/parameters body.
func convertImagenChatBody(body []byte) ([]byte, error) {
	prompt := imagenPrompt(body)
	if prompt == "" {
		return nil, invalidImage("Imagen requires a prompt")
	}
	count := int64(1)
	for _, path := range []string{"n", "extra_body.n"} {
		if n := gjson.GetBytes(body, path); n.Exists() {
			if n.Type != gjson.Number || n.Float() != float64(n.Int()) || n.Int() < 1 || n.Int() > 4 {
				return nil, invalidImage("Imagen n must be an integer between 1 and 4")
			}
			count = n.Int()
		}
	}
	size, err := imagenAspectRatio(body)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"instances": []map[string]string{{"prompt": prompt}},
		"parameters": map[string]any{"sampleCount": count, "aspectRatio": size, "personGeneration": "allow_adult"}})
}

// imagenPrompt extracts the generation prompt from the first user message's
// text content, falling back to a top-level "prompt" field.
func imagenPrompt(body []byte) string {
	prompt := ""
	for _, message := range gjson.GetBytes(body, "messages").Array() {
		if message.Get("role").Str != "user" {
			continue
		}
		content := message.Get("content")
		if content.Type == gjson.String {
			prompt = content.Str
		} else {
			for _, part := range content.Array() {
				if part.Get("type").Str == "text" {
					prompt += part.Get("text").Str
				}
			}
		}
		if prompt != "" {
			break
		}
	}
	if prompt == "" {
		prompt = gjson.GetBytes(body, "prompt").Str
	}
	return prompt
}

func imagenAspectRatio(body []byte) (string, error) {
	size := "1:1"
	for _, path := range []string{"size", "extra_body.size", "extra_body.aspectRatio", "extra_body.parameters.aspectRatio"} {
		if value := gjson.GetBytes(body, path).Str; value != "" {
			size = value
		}
	}
	if mapped := map[string]string{"256x256": "1:1", "512x512": "1:1", "1024x1024": "1:1", "1536x1024": "3:2", "1024x1536": "2:3", "1024x1792": "9:16", "1792x1024": "16:9"}[size]; mapped != "" {
		size = mapped
	}
	if size != "1:1" && size != "3:2" && size != "2:3" && size != "9:16" && size != "16:9" {
		return "", invalidImage("Imagen size must be a supported aspect ratio")
	}
	return size, nil
}

type imageStream struct {
	body io.ReadCloser
	req  *gateway.Request
	done bool
}

func (s *imageStream) Close() error { return s.body.Close() }

func (s *imageStream) Next() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	s.done = true
	data, err := io.ReadAll(io.LimitReader(s.body, (64<<20)+1))
	if err != nil || len(data) > 64<<20 {
		return gateway.Event{}, errors.New("vertex: cannot read image response")
	}
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return gateway.Event{}, errors.New("vertex: invalid image response JSON")
	}
	if e := gjson.GetBytes(data, "error"); e.Exists() {
		status := int(e.Get("code").Int())
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		return gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{Status: status, Type: "upstream_error", Code: e.Get("status").Str, Message: e.Get("message").Str}}, nil
	}
	images := []map[string]string{}
	for _, prediction := range gjson.GetBytes(data, "predictions").Array() {
		if prediction.Get("raiFilteredReason").Str != "" {
			continue
		}
		encoded := prediction.Get("bytesBase64Encoded").Str
		if encoded == "" {
			return gateway.Event{}, errors.New("vertex: image prediction is missing data")
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(decoded) == 0 {
			return gateway.Event{}, errors.New("vertex: image prediction has invalid base64")
		}
		images = append(images, map[string]string{"b64_json": encoded})
	}
	if len(images) == 0 {
		return gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{Status: http.StatusBadRequest,
			Type: "invalid_request_error", Code: "content_filter", Message: "Imagen returned no generated images"}}, nil
	}
	tokens := int64(len(images)) * 258
	u := &gateway.Usage{PromptTokens: tokens, ImageOutputTokens: tokens, Estimated: true}
	if meta := gjson.GetBytes(data, "usageMetadata"); meta.Exists() {
		u = &gateway.Usage{PromptTokens: meta.Get("promptTokenCount").Int(), CompletionTokens: meta.Get("candidatesTokenCount").Int() + meta.Get("thoughtsTokenCount").Int(), CachedTokens: meta.Get("cachedContentTokenCount").Int()}
		for _, detail := range meta.Get("candidatesTokensDetails").Array() {
			if strings.EqualFold(detail.Get("modality").Str, "IMAGE") {
				u.ImageOutputTokens += detail.Get("tokenCount").Int()
			}
		}
	}
	u.ImageCount = int64(len(images))
	if s.req.Protocol != gateway.ProtocolGemini {
		data, err = json.Marshal(map[string]any{"created": time.Now().Unix(), "data": images})
	}
	return gateway.Event{Kind: gateway.EventData, Payload: data, Usage: u}, err
}
