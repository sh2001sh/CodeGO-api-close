package doubao

import (
	"encoding/json"
	"errors"
	"strconv"
)

func requestBody(data []byte, model string) ([]byte, error) {
	var source map[string]json.RawMessage
	if json.Unmarshal(data, &source) != nil || source == nil {
		return nil, errors.New("invalid doubao request")
	}
	if model == "" {
		return nil, errors.New("missing upstream model")
	}
	body := passthroughFields(source)
	if err := mergeDoubaoMetadata(source, body); err != nil {
		return nil, err
	}
	content, err := resolveDoubaoContent(source, body)
	if err != nil {
		return nil, err
	}
	content, err = applyDoubaoPrompt(source, content)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, errors.New("doubao requires prompt or content")
	}
	body["content"], _ = json.Marshal(content)
	if err := applyDoubaoSeconds(source, body); err != nil {
		return nil, err
	}
	if err := normalizeDoubaoIntegers(body); err != nil {
		return nil, err
	}
	if err := normalizeDoubaoBooleans(body); err != nil {
		return nil, err
	}
	body["model"], _ = json.Marshal(model)
	return json.Marshal(body)
}

// passthroughFields copies the fields that are forwarded to upstream
// unchanged, when present in source.
func passthroughFields(source map[string]json.RawMessage) map[string]json.RawMessage {
	body := make(map[string]json.RawMessage)
	for _, key := range []string{"content", "callback_url", "return_last_frame", "service_tier", "execution_expires_after", "generate_audio", "draft", "tools", "resolution", "ratio", "duration", "frames", "seed", "camera_fixed", "watermark"} {
		if value, ok := source[key]; ok {
			body[key] = value
		}
	}
	return body
}

// mergeDoubaoMetadata unpacks the "metadata" field, which may be a
// JSON-encoded string, and merges its entries (other than "model") into body.
func mergeDoubaoMetadata(source, body map[string]json.RawMessage) error {
	value, ok := source["metadata"]
	if !ok {
		return nil
	}
	var metadata map[string]json.RawMessage
	var text string
	if json.Unmarshal(value, &text) == nil {
		value = json.RawMessage(text)
	}
	if json.Unmarshal(value, &metadata) != nil {
		return errors.New("invalid doubao metadata")
	}
	for key, value := range metadata {
		if key != "model" {
			body[key] = value
		}
	}
	return nil
}

// resolveDoubaoContent returns the explicit "content" array if present,
// otherwise builds image_url content items from "images" or "image".
func resolveDoubaoContent(source, body map[string]json.RawMessage) ([]map[string]json.RawMessage, error) {
	var content []map[string]json.RawMessage
	if value, ok := body["content"]; ok {
		if json.Unmarshal(value, &content) != nil {
			return nil, errors.New("invalid doubao content")
		}
		return content, nil
	}
	var images []string
	if value, ok := source["images"]; ok && json.Unmarshal(value, &images) != nil {
		return nil, errors.New("invalid doubao images")
	}
	if len(images) == 0 {
		var image string
		if value, ok := source["image"]; ok {
			if json.Unmarshal(value, &image) != nil {
				return nil, errors.New("invalid doubao image")
			}
			if image != "" {
				images = []string{image}
			}
		}
	}
	for _, image := range images {
		link, _ := json.Marshal(map[string]string{"url": image})
		content = append(content, map[string]json.RawMessage{"type": json.RawMessage(`"image_url"`), "image_url": link})
	}
	return content, nil
}

// applyDoubaoPrompt replaces any existing text content entry with the
// compatibility request's "prompt", if provided.
func applyDoubaoPrompt(source map[string]json.RawMessage, content []map[string]json.RawMessage) ([]map[string]json.RawMessage, error) {
	prompt, ok := source["prompt"]
	if !ok {
		return content, nil
	}
	var text string
	if json.Unmarshal(prompt, &text) != nil {
		return nil, errors.New("invalid doubao prompt")
	}
	filtered := make([]map[string]json.RawMessage, 0, len(content)+1)
	for _, item := range content {
		var kind string
		if json.Unmarshal(item["type"], &kind) != nil {
			return nil, errors.New("invalid doubao content type")
		}
		if kind != "text" {
			filtered = append(filtered, item)
		}
	}
	return append(filtered, map[string]json.RawMessage{"type": json.RawMessage(`"text"`), "text": prompt}), nil
}

// applyDoubaoSeconds converts the compatibility "seconds" field into the
// upstream "duration" field.
func applyDoubaoSeconds(source, body map[string]json.RawMessage) error {
	value, ok := source["seconds"]
	if !ok {
		return nil
	}
	var seconds string
	if json.Unmarshal(value, &seconds) != nil {
		return errors.New("invalid doubao seconds")
	}
	if duration, err := strconv.Atoi(seconds); err == nil && duration > 0 {
		body["duration"] = json.RawMessage(strconv.Itoa(duration))
	}
	return nil
}

// normalizeDoubaoIntegers coerces integer-typed fields that may have
// arrived as JSON strings into plain JSON numbers.
func normalizeDoubaoIntegers(body map[string]json.RawMessage) error {
	for _, key := range []string{"duration", "frames", "seed", "execution_expires_after"} {
		value, ok := body[key]
		if !ok {
			continue
		}
		var number int
		if json.Unmarshal(value, &number) != nil {
			var text string
			if json.Unmarshal(value, &text) != nil {
				return errors.New("invalid doubao integer field")
			}
			var err error
			number, err = strconv.Atoi(text)
			if err != nil {
				return errors.New("invalid doubao integer field")
			}
		}
		body[key] = json.RawMessage(strconv.Itoa(number))
	}
	return nil
}

// normalizeDoubaoBooleans coerces boolean-typed fields that may have arrived
// as JSON strings ("true"/"false") into plain JSON booleans.
func normalizeDoubaoBooleans(body map[string]json.RawMessage) error {
	for _, key := range []string{"return_last_frame", "generate_audio", "draft", "camera_fixed", "watermark"} {
		value, ok := body[key]
		if !ok {
			continue
		}
		var boolean bool
		if json.Unmarshal(value, &boolean) != nil {
			var text string
			if json.Unmarshal(value, &text) != nil || (text != "true" && text != "false") {
				return errors.New("invalid doubao boolean field")
			}
			boolean = text == "true"
		}
		body[key] = json.RawMessage(strconv.FormatBool(boolean))
	}
	return nil
}
