package replicate

import (
	"encoding/json"
	"strconv"
	"strings"
)

func convertImageRequest(data []byte, version string) ([]byte, error) {
	fields, err := object(data)
	if err != nil {
		return nil, err
	}
	input := map[string]json.RawMessage{}
	if err := applyPrompt(fields, input); err != nil {
		return nil, err
	}
	if err := applySize(fields, input); err != nil {
		return nil, err
	}
	if err := applyCount(fields, input); err != nil {
		return nil, err
	}
	if err := applyQuality(fields, input); err != nil {
		return nil, err
	}
	if err := applyOutputFormat(fields, input); err != nil {
		return nil, err
	}
	if err := mergeExtraInput(fields, input); err != nil {
		return nil, err
	}
	// v2 places unknown image fields in native input. Known OpenAI-only image
	// fields remain outside it; native input/extra_fields can supply equivalents.
	for key, value := range fields {
		switch key {
		case "model", "prompt", "n", "size", "quality", "response_format", "style", "user", "extra_fields", "background", "moderation", "output_format", "output_compression", "partial_images", "stream", "images", "mask", "input_fidelity", "watermark", "watermark_enabled", "user_id", "image", "input":
		default:
			input[key] = value
		}
	}
	var prompt string
	if json.Unmarshal(input["prompt"], &prompt) != nil || strings.TrimSpace(prompt) == "" {
		return nil, invalid("invalid_prompt", "replicate image generation requires a prompt")
	}
	if raw, exists := input["image_prompt"]; exists {
		var image string
		if json.Unmarshal(raw, &image) != nil || strings.TrimSpace(image) == "" {
			return nil, invalid("invalid_image", "replicate image_prompt must be an uploaded image URL")
		}
	}
	out := map[string]any{"input": input}
	if version != "" {
		out["version"] = version
	}
	return json.Marshal(out)
}

func applyPrompt(fields, input map[string]json.RawMessage) error {
	var prompt string
	if raw, exists := fields["prompt"]; exists {
		if json.Unmarshal(raw, &prompt) != nil {
			return invalid("invalid_prompt", "replicate image prompt must be text")
		}
	}
	if strings.TrimSpace(prompt) != "" {
		input["prompt"], _ = json.Marshal(prompt)
	}
	return nil
}

func applySize(fields, input map[string]json.RawMessage) error {
	raw, exists := fields["size"]
	if !exists || string(raw) == "null" {
		return nil
	}
	var size string
	if json.Unmarshal(raw, &size) != nil {
		return invalid("invalid_size", "replicate image size must be text")
	}
	aspect, width, height, ok := mapSize(size)
	if !ok {
		return nil
	}
	input["aspect_ratio"], _ = json.Marshal(aspect)
	if aspect == "custom" {
		input["width"], _ = json.Marshal(width)
		input["height"], _ = json.Marshal(height)
	}
	return nil
}

func applyCount(fields, input map[string]json.RawMessage) error {
	raw, exists := fields["n"]
	if !exists || string(raw) == "null" {
		return nil
	}
	var count int
	if json.Unmarshal(raw, &count) != nil || count < 1 || count > 16 {
		return invalid("invalid_n", "replicate image count must be between 1 and 16")
	}
	input["num_outputs"] = raw
	return nil
}

func applyQuality(fields, input map[string]json.RawMessage) error {
	raw := fields["quality"]
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var quality string
	if json.Unmarshal(raw, &quality) != nil {
		return invalid("invalid_quality", "replicate image quality must be text")
	}
	if strings.EqualFold(quality, "hd") || strings.EqualFold(quality, "high") {
		input["prompt_upsampling"] = json.RawMessage("true")
	}
	return nil
}

func applyOutputFormat(fields, input map[string]json.RawMessage) error {
	raw := fields["output_format"]
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var format string
	if json.Unmarshal(raw, &format) != nil {
		return invalid("invalid_output_format", "replicate output_format must be text")
	}
	if strings.TrimSpace(format) != "" {
		input["output_format"] = raw
	}
	return nil
}

func mergeExtraInput(fields, input map[string]json.RawMessage) error {
	for _, name := range []string{"extra_fields", "input"} {
		raw, exists := fields[name]
		if !exists {
			continue
		}
		var extra map[string]json.RawMessage
		if json.Unmarshal(raw, &extra) != nil || extra == nil {
			return invalid("invalid_input", "replicate "+name+" must be an object")
		}
		for key, value := range extra {
			input[key] = value
		}
	}
	return nil
}

func mapSize(size string) (string, int, int, bool) {
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "", 0, 0, false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return "", 0, 0, false
	}
	switch {
	case w == h:
		return "1:1", 0, 0, true
	case w == 1792 && h == 1024:
		return "16:9", 0, 0, true
	case w == 1024 && h == 1792:
		return "9:16", 0, 0, true
	case w == 1536 && h == 1024:
		return "3:2", 0, 0, true
	case w == 1024 && h == 1536:
		return "2:3", 0, 0, true
	}
	a, b := w, h
	for b != 0 {
		a, b = b, a%b
	}
	ratio := strconv.Itoa(w/a) + ":" + strconv.Itoa(h/a)
	switch ratio {
	case "1:1", "16:9", "9:16", "3:2", "2:3", "4:5", "5:4", "3:4", "4:3":
		return ratio, 0, 0, true
	}
	return "custom", normalizeDimension(w), normalizeDimension(h), true
}

func normalizeDimension(value int) int {
	value = min(1440, max(256, value))
	if remainder := value % 32; remainder >= 16 {
		value += 32 - remainder
	} else {
		value -= remainder
	}
	return min(1440, max(256, value))
}
