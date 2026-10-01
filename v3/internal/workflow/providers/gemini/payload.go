package gemini

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

// Payload converts the shared video submission to Google's Veo request shape.
// Vertex uses the same native payload.
func Payload(input native.Submit) ([]byte, error) {
	fields, image, err := readFields(input)
	if err != nil {
		return nil, err
	}
	if instances, ok := fields["instances"].([]any); ok && len(instances) > 0 {
		params, err := veoParameters(fields, true)
		if err != nil {
			return nil, err
		}
		fields["parameters"] = params
		delete(fields, "model")
		return json.Marshal(fields)
	}
	prompt, _ := fields["prompt"].(string)
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("veo prompt required")
	}
	instance := map[string]any{"prompt": prompt}
	if image == nil {
		var value string
		if images, ok := fields["images"].([]any); ok && len(images) > 0 {
			value, _ = images[0].(string)
		}
		if value == "" {
			value, _ = fields["input_reference"].(string)
		}
		if value != "" {
			image, err = parseImage(value)
			if err != nil {
				return nil, err
			}
		}
	}
	if image != nil {
		instance["image"] = image
	}
	params, err := veoParameters(fields, false)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"instances": []any{instance}, "parameters": params})
}

func integer(value any) (int, error) {
	var n int
	switch v := value.(type) {
	case float64:
		if v != float64(int(v)) {
			return 0, errors.New("invalid Veo duration")
		}
		n = int(v)
	case string:
		var err error
		n, err = strconv.Atoi(v)
		if err != nil {
			return 0, errors.New("invalid Veo duration")
		}
	case int:
		n = v
	default:
		return 0, errors.New("invalid Veo duration")
	}
	if n <= 0 || n > 3600 {
		return 0, errors.New("invalid Veo duration")
	}
	return n, nil
}

func sizeParameters(size string) (string, string, error) {
	size = strings.ToLower(strings.ReplaceAll(size, "*", "x"))
	switch size {
	case "", "720p", "1080p", "4k":
		if size == "" {
			size = "720p"
		}
		return size, "16:9", nil
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "", "", errors.New("invalid Veo size")
	}
	width, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || width <= 0 {
		return "", "", errors.New("invalid Veo size")
	}
	height, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || height <= 0 {
		return "", "", errors.New("invalid Veo size")
	}
	resolution, ratio := "720p", "16:9"
	if width >= 3840 || height >= 3840 {
		resolution = "4k"
	} else if width >= 1920 || height >= 1920 {
		resolution = "1080p"
	}
	if height > width {
		ratio = "9:16"
	}
	return resolution, ratio, nil
}
