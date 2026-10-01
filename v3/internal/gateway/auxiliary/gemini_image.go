package auxiliary

import (
	"encoding/json"
	"strings"
)

func geminiImageRequest(body map[string]json.RawMessage, model string) (map[string]json.RawMessage, error) {
	if !strings.HasPrefix(model, "imagen") {
		return nil, geminiInvalid("only Imagen models support image generation")
	}
	var prompt string
	if json.Unmarshal(body["prompt"], &prompt) != nil || strings.TrimSpace(prompt) == "" {
		return nil, geminiInvalid("prompt is required")
	}
	n := 1
	if raw, ok := body["n"]; ok && (json.Unmarshal(raw, &n) != nil || n <= 0) {
		return nil, geminiInvalid("n must be a positive integer")
	}
	var size, quality, format string
	for key, dst := range map[string]*string{"size": &size, "quality": &quality, "response_format": &format} {
		if raw, ok := body[key]; ok && json.Unmarshal(raw, dst) != nil {
			return nil, geminiInvalid(key + " must be a string")
		}
	}
	if format != "" && format != "b64_json" {
		return nil, geminiInvalid("Imagen supports response_format=b64_json")
	}
	if _, ok := body["image"]; ok {
		return nil, geminiInvalid("Imagen image generation does not support reference images")
	}
	if _, ok := body["mask"]; ok {
		return nil, geminiInvalid("Imagen image generation does not support masks")
	}
	aspect := "1:1"
	if strings.Contains(size, ":") {
		aspect = size
	} else {
		switch strings.TrimSpace(size) {
		case "1536x1024":
			aspect = "3:2"
		case "1024x1536":
			aspect = "2:3"
		case "1024x1792":
			aspect = "9:16"
		case "1792x1024":
			aspect = "16:9"
		}
	}
	parameters := map[string]any{"sampleCount": n, "aspectRatio": aspect, "personGeneration": "allow_adult"}
	if quality != "" {
		parameters["imageSize"] = "1K"
		if quality == "hd" || quality == "high" || quality == "2K" {
			parameters["imageSize"] = "2K"
		}
	}
	instances, err := json.Marshal([]map[string]string{{"prompt": prompt}})
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(parameters)
	return map[string]json.RawMessage{"instances": instances, "parameters": encoded}, err
}
