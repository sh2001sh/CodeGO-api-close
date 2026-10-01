package vidu

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func requestBody(input native.Submit, model string) ([]byte, string, error) {
	var source map[string]json.RawMessage
	if json.Unmarshal(input.Body, &source) != nil || source == nil {
		return nil, "", errors.New("invalid vidu request")
	}
	if model == "" {
		return nil, "", errors.New("missing upstream model")
	}
	body := viduDefaults()
	if err := copyViduFields(source, body); err != nil {
		return nil, "", err
	}
	if err := resolveViduImage(source, body); err != nil {
		return nil, "", err
	}
	if err := normalizeViduDuration(body); err != nil {
		return nil, "", err
	}
	metadata, err := mergeViduMetadata(source, body)
	if err != nil {
		return nil, "", err
	}
	action, err := resolveViduAction(input.Action, metadata, body)
	if err != nil {
		return nil, "", err
	}
	body["model"], _ = json.Marshal(model)
	data, err := json.Marshal(body)
	return data, action, err
}

// viduDefaults returns the baseline upstream fields before source overrides.
func viduDefaults() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"duration": json.RawMessage("5"), "resolution": json.RawMessage(`"1080p"`),
		"movement_amplitude": json.RawMessage(`"auto"`), "bgm": json.RawMessage("false"),
	}
}

// copyViduFields copies the passthrough fields from source, then applies
// the "size" -> "resolution" alias.
func copyViduFields(source, body map[string]json.RawMessage) error {
	for _, key := range []string{"images", "prompt", "duration", "seed", "resolution", "movement_amplitude", "bgm", "payload", "callback_url"} {
		if value, ok := source[key]; ok {
			body[key] = value
		}
	}
	if size, ok := source["size"]; ok {
		var value string
		if json.Unmarshal(size, &value) != nil {
			return errors.New("invalid vidu size")
		}
		if value != "" {
			body["resolution"] = size
		}
	}
	return nil
}

// resolveViduImage fills in "images" from a single "image" field when no
// images array is already present.
func resolveViduImage(source, body map[string]json.RawMessage) error {
	var images []string
	if value, ok := body["images"]; ok && json.Unmarshal(value, &images) != nil {
		return errors.New("invalid vidu images")
	}
	if len(images) > 0 {
		return nil
	}
	var image string
	if value, ok := source["image"]; ok {
		if json.Unmarshal(value, &image) != nil {
			return errors.New("invalid vidu image")
		}
		if image != "" {
			body["images"], _ = json.Marshal([]string{image})
		}
	}
	return nil
}

// normalizeViduDuration coerces "duration" into a positive integer string,
// defaulting to 5 when zero or negative.
func normalizeViduDuration(body map[string]json.RawMessage) error {
	value, ok := body["duration"]
	if !ok {
		return nil
	}
	var duration int
	if json.Unmarshal(value, &duration) != nil {
		var text string
		if json.Unmarshal(value, &text) != nil {
			return errors.New("invalid vidu duration")
		}
		var err error
		duration, err = strconv.Atoi(text)
		if err != nil {
			return errors.New("invalid vidu duration")
		}
	}
	if duration <= 0 {
		duration = 5
	}
	body["duration"] = json.RawMessage(strconv.Itoa(duration))
	return nil
}

// mergeViduMetadata unpacks the "metadata" field, which may itself be a
// JSON-encoded string, merges its entries (other than "action"/"model")
// into body, and returns the parsed metadata map for action resolution.
func mergeViduMetadata(source, body map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	value, ok := source["metadata"]
	if !ok {
		return nil, nil
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		value = json.RawMessage(text)
	}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(value, &metadata) != nil {
		return nil, errors.New("invalid vidu metadata")
	}
	for key, value := range metadata {
		if key != "action" && key != "model" {
			body[key] = value
		}
	}
	return metadata, nil
}

// resolveViduAction determines the upstream action: an explicit
// metadata.action override, or one inferred from the resolved image count.
func resolveViduAction(inputAction string, metadata, body map[string]json.RawMessage) (string, error) {
	action := inputAction
	if value, ok := metadata["action"]; ok {
		if json.Unmarshal(value, &action) != nil {
			return "", errors.New("invalid vidu action")
		}
		return action, nil
	}
	if action != "" && action != "generate" {
		return action, nil
	}
	var images []string
	if json.Unmarshal(body["images"], &images) == nil && len(images) > 0 {
		action = "generate"
		if len(images) == 2 {
			action = "firstTailGenerate"
		}
		if len(images) > 2 {
			action = "referenceGenerate"
		}
	} else {
		action = "textGenerate"
	}
	return action, nil
}
