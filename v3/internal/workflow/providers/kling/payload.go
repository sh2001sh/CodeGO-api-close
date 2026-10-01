package kling

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func requestBody(input native.Submit, upstream string) ([]byte, string, error) {
	fields, err := requestFields(input)
	if err != nil {
		return nil, "", err
	}
	if err := mergeKlingMetadata(fields); err != nil {
		return nil, "", err
	}
	applyKlingModel(fields, upstream, input.Model)
	if err := applyKlingDuration(fields); err != nil {
		return nil, "", err
	}
	applyKlingAspectRatio(fields)
	action, err := resolveKlingImages(fields)
	if err != nil {
		return nil, "", err
	}
	for _, k := range []string{"metadata", "size", "seconds", "input_reference", "images"} {
		delete(fields, k)
	}
	body, err := json.Marshal(fields)
	return body, action, err
}

// mergeKlingMetadata unpacks the "metadata" field, which may itself be a
// JSON-encoded string, and merges its contents into fields.
func mergeKlingMetadata(fields map[string]json.RawMessage) error {
	metadata := fields["metadata"]
	if len(metadata) == 0 || string(metadata) == "null" {
		return nil
	}
	var encoded string
	if json.Unmarshal(metadata, &encoded) == nil {
		metadata = []byte(encoded)
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &extra); err != nil || extra == nil {
		return errors.New("invalid kling metadata")
	}
	for k, v := range extra {
		fields[k] = v
	}
	return nil
}

// applyKlingModel sets model_name/model and the mode/cfg_scale defaults.
func applyKlingModel(fields map[string]json.RawMessage, upstream, inputModel string) {
	if upstream == "" {
		upstream = inputModel
		if upstream == "" {
			upstream = "kling-v1"
		}
	}
	fields["model_name"], _ = json.Marshal(upstream)
	fields["model"], _ = json.Marshal(upstream)
	if _, ok := fields["mode"]; !ok {
		fields["mode"] = json.RawMessage(`"std"`)
	}
	if _, ok := fields["cfg_scale"]; !ok {
		fields["cfg_scale"] = json.RawMessage(`0.5`)
	}
}

// applyKlingDuration normalizes "duration" (falling back to "seconds", then
// a default of 5) into the string form kling expects.
func applyKlingDuration(fields map[string]json.RawMessage) error {
	duration := fields["duration"]
	if len(duration) == 0 {
		duration = fields["seconds"]
	}
	if len(duration) == 0 {
		duration = json.RawMessage(`5`)
	}
	var value string
	if json.Unmarshal(duration, &value) != nil {
		var n int
		if err := json.Unmarshal(duration, &n); err != nil {
			return errors.New("invalid kling duration")
		}
		value = strconv.Itoa(n)
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return errors.New("invalid kling duration")
	}
	fields["duration"], _ = json.Marshal(value)
	return nil
}

// applyKlingAspectRatio defaults "aspect_ratio" based on the requested
// "size", when not explicitly provided.
func applyKlingAspectRatio(fields map[string]json.RawMessage) {
	if _, ok := fields["aspect_ratio"]; ok {
		return
	}
	ratio := "1:1"
	var size string
	_ = json.Unmarshal(fields["size"], &size)
	switch size {
	case "1280x720", "1920x1080":
		ratio = "16:9"
	case "720x1280", "1080x1920":
		ratio = "9:16"
	}
	fields["aspect_ratio"], _ = json.Marshal(ratio)
}

// resolveKlingImages fills in "image"/"image_tail" from "input_reference" or
// "images" when not already set, and returns the resulting action
// ("generate" if any image is present, otherwise "textGenerate").
func resolveKlingImages(fields map[string]json.RawMessage) (string, error) {
	var image, tail string
	if v := fields["image"]; len(v) > 0 && json.Unmarshal(v, &image) != nil {
		return "", errors.New("invalid kling image")
	}
	if v := fields["image_tail"]; len(v) > 0 && json.Unmarshal(v, &tail) != nil {
		return "", errors.New("invalid kling image tail")
	}
	if image == "" {
		if v := fields["input_reference"]; len(v) > 0 && json.Unmarshal(v, &image) != nil {
			return "", errors.New("invalid kling input reference")
		}
		if image != "" {
			fields["image"], _ = json.Marshal(image)
		}
	}
	if image == "" && len(fields["images"]) > 0 {
		var images []string
		if err := json.Unmarshal(fields["images"], &images); err != nil || len(images) > 2 {
			return "", errors.New("invalid kling image list")
		}
		if len(images) > 0 {
			image = images[0]
			fields["image"], _ = json.Marshal(image)
		}
		if len(images) > 1 && tail == "" {
			tail = images[1]
			fields["image_tail"], _ = json.Marshal(tail)
		}
	}
	action := "textGenerate"
	if strings.TrimSpace(image) != "" || strings.TrimSpace(tail) != "" {
		action = "generate"
	}
	return action, nil
}
