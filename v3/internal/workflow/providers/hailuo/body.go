package hailuo

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

func requestBody(data []byte, model string) ([]byte, error) {
	var source map[string]json.RawMessage
	if json.Unmarshal(data, &source) != nil || source == nil {
		return nil, errors.New("invalid hailuo request")
	}
	if model == "" {
		return nil, errors.New("missing upstream model")
	}
	body := hailuoDefaults(model)
	copyHailuoFields(source, body)
	if err := applyHailuoSize(source, body); err != nil {
		return nil, err
	}
	if err := mergeHailuoMetadata(source, body); err != nil {
		return nil, err
	}
	if err := normalizeHailuoDuration(body); err != nil {
		return nil, err
	}
	body["model"], _ = json.Marshal(model)
	return json.Marshal(body)
}

// hailuoDefaults returns the baseline upstream fields before source
// overrides, including the model-dependent default resolution.
func hailuoDefaults(model string) map[string]json.RawMessage {
	resolution := "720P"
	if model == "MiniMax-Hailuo-02" || model == "MiniMax-Hailuo-2.3" || model == "MiniMax-Hailuo-2.3-Fast" || model == "T2V-01-Director" {
		resolution = "768P"
	}
	body := map[string]json.RawMessage{"duration": json.RawMessage("6")}
	body["resolution"], _ = json.Marshal(resolution)
	return body
}

// copyHailuoFields copies the passthrough fields from source into body.
func copyHailuoFields(source, body map[string]json.RawMessage) {
	for _, key := range []string{"prompt", "prompt_optimizer", "fast_pretreatment", "duration", "resolution", "callback_url", "aigc_watermark", "first_frame_image", "last_frame_image", "subject_reference"} {
		if value, ok := source[key]; ok {
			body[key] = value
		}
	}
}

// applyHailuoSize maps a "size" field containing a resolution hint onto the
// upstream "resolution" field.
func applyHailuoSize(source, body map[string]json.RawMessage) error {
	value, ok := source["size"]
	if !ok {
		return nil
	}
	var size string
	if json.Unmarshal(value, &size) != nil {
		return errors.New("invalid hailuo size")
	}
	for _, candidate := range []string{"1080", "768", "720", "512"} {
		if strings.Contains(size, candidate) {
			body["resolution"], _ = json.Marshal(candidate + "P")
			break
		}
	}
	return nil
}

// mergeHailuoMetadata unpacks the "metadata" field, which may itself be a
// JSON-encoded string, and merges its contents (other than "model") into
// body.
func mergeHailuoMetadata(source, body map[string]json.RawMessage) error {
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
		return errors.New("invalid hailuo metadata")
	}
	for key, value := range metadata {
		if key != "model" {
			body[key] = value
		}
	}
	return nil
}

// normalizeHailuoDuration coerces "duration" into a positive integer
// string, defaulting to 6 when zero or negative.
func normalizeHailuoDuration(body map[string]json.RawMessage) error {
	value, ok := body["duration"]
	if !ok {
		return nil
	}
	var duration int
	if json.Unmarshal(value, &duration) != nil {
		var text string
		if json.Unmarshal(value, &text) != nil {
			return errors.New("invalid hailuo duration")
		}
		var err error
		duration, err = strconv.Atoi(text)
		if err != nil {
			return errors.New("invalid hailuo duration")
		}
	}
	if duration <= 0 {
		duration = 6
	}
	body["duration"] = json.RawMessage(strconv.Itoa(duration))
	return nil
}
