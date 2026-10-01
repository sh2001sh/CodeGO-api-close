package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
)

func convertContent(raw json.RawMessage) ([]block, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if text == "" {
			return nil, nil
		}
		return []block{{Type: "text", Text: text}}, nil
	}
	value := gjson.ParseBytes(raw)
	if !value.IsArray() {
		return nil, fmt.Errorf("anthropic: content must be a string or block array")
	}
	var out []block
	for _, part := range value.Array() {
		allowed := "text"
		if part.Get("type").Str == "image_url" {
			allowed = "image_url"
		}
		unsupported := unknownField(part, "type", allowed)
		if unsupported != "" {
			return nil, fmt.Errorf("anthropic: unsupported content field %q", unsupported)
		}
		switch part.Get("type").Str {
		case "text":
			if part.Get("text").Type != gjson.String {
				return nil, fmt.Errorf("anthropic: text block requires text")
			}
			if text := part.Get("text").Str; text != "" {
				out = append(out, block{Type: "text", Text: text})
			}
		case "image_url":
			if bad := unknownField(part.Get("image_url"), "url", "detail"); bad != "" {
				return nil, fmt.Errorf("anthropic: unsupported image field %q", bad)
			}
			if detail := part.Get("image_url.detail").Str; detail != "" && detail != "auto" {
				return nil, fmt.Errorf("anthropic: image detail %q is unsupported", detail)
			}
			src, err := convertImage(part.Get("image_url.url").Str)
			if err != nil {
				return nil, err
			}
			out = append(out, block{Type: "image", Source: src})
		default:
			return nil, fmt.Errorf("anthropic: unsupported content block %q", part.Get("type").Str)
		}
	}
	return out, nil
}

func convertImage(value string) (*source, error) {
	if strings.HasPrefix(value, "data:") {
		meta, data, ok := strings.Cut(strings.TrimPrefix(value, "data:"), ",")
		if !ok || !strings.HasSuffix(meta, ";base64") {
			return nil, fmt.Errorf("anthropic: image data must use base64")
		}
		media := strings.TrimSuffix(meta, ";base64")
		switch media {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
		default:
			return nil, fmt.Errorf("anthropic: unsupported image type %q", media)
		}
		if _, err := base64.StdEncoding.DecodeString(data); err != nil {
			return nil, fmt.Errorf("anthropic: invalid base64 image: %w", err)
		}
		return &source{Type: "base64", MediaType: media, Data: data}, nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("anthropic: image URL must be HTTP(S) or base64 data")
	}
	return &source{Type: "url", URL: value}, nil
}
