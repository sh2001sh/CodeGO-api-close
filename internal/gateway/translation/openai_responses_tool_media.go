package translation

import (
	"encoding/json"
	"strings"

	"github.com/sh2001sh/new-api/dto"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

// Chat tool messages carry text. Move image/file parts to the following user
// message only after all parallel tool outputs, preserving call/output order.
func responsesToolOutputToChat(raw json.RawMessage) (string, []dto.MediaContent, error) {
	if platformencoding.GetJSONType(raw) == "string" {
		text := rawString(raw)
		if strings.HasPrefix(text, "data:image/") {
			return "", []dto.MediaContent{{Type: dto.ContentTypeImageURL, ImageUrl: map[string]any{"url": text}}}, nil
		}
		if json.Valid([]byte(text)) {
			return responsesToolOutputToChat(json.RawMessage(text))
		}
		return text, nil, nil
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		var item map[string]json.RawMessage
		if json.Unmarshal(raw, &item) != nil || item == nil {
			return responsesToolOutputText(raw), nil, nil
		}
		if content, ok := item["content"]; ok {
			text, media, err := responsesToolOutputToChat(content)
			if err != nil {
				return "", nil, err
			}
			if len(media) == 0 {
				return responsesToolOutputText(raw), nil, nil
			}
			delete(item, "content")
			if len(item) > 0 {
				rest, _ := json.Marshal(item)
				text = string(rest) + "\n" + text
			}
			return text, media, nil
		}
		parts = []json.RawMessage{raw}
	}
	var text strings.Builder
	var media []dto.MediaContent
	for _, part := range parts {
		var item map[string]json.RawMessage
		if json.Unmarshal(part, &item) != nil {
			text.Write(part)
			continue
		}
		switch rawString(item["type"]) {
		case "input_image", "image_url", "input_file", "file":
			wrapped, _ := json.Marshal([]json.RawMessage{part})
			content, err := responsesContentToChat(wrapped)
			if err != nil {
				return "", nil, err
			}
			media = append(media, content.([]dto.MediaContent)...)
		case "input_text", "output_text", "text":
			text.WriteString(rawString(item["text"]))
		default:
			text.Write(part)
		}
	}
	if len(media) == 0 {
		return responsesToolOutputText(raw), nil, nil
	}
	return text.String(), media, nil
}
