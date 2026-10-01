package gemini

import (
	"encoding/json"
	"errors"
	"strings"
)

// BillingParameters shares the outgoing Veo parameter parser. The returned
// duration is physical seconds; the model-specific resolution factor is exact
// v2 ppm, including 2333333 for fast 4K (not the repeating fraction 7/3).
func BillingParameters(body []byte, model string) (int64, int64, error) {
	var fields map[string]any
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return 0, 0, errors.New("invalid Veo billing JSON")
	}
	instances, ok := fields["instances"].([]any)
	native := ok && len(instances) > 0
	params, err := veoParameters(fields, native)
	if err != nil {
		return 0, 0, err
	}
	ppm := int64(1_000_000)
	if params["resolution"] == "4k" {
		switch {
		case strings.Contains(model, "3.1-fast-generate"):
			ppm = 2_333_333
		case strings.Contains(model, "3.1"):
			ppm = 1_500_000
		}
	}
	return int64(params["durationSeconds"].(int)), ppm, nil
}

func veoParameters(fields map[string]any, native bool) (map[string]any, error) {
	params, err := veoBaseParameters(fields, native)
	if err != nil {
		return nil, err
	}
	if err := applyVeoDuration(fields, params); err != nil {
		return nil, err
	}
	if err := applyVeoResolution(fields, params); err != nil {
		return nil, err
	}
	if !native {
		params["sampleCount"] = 1
	}
	return params, nil
}

// veoBaseParameters extracts the "metadata" (compatibility) or "parameters"
// (native) object from fields, which may itself be a JSON-encoded string.
func veoBaseParameters(fields map[string]any, native bool) (map[string]any, error) {
	params := make(map[string]any)
	key := "metadata"
	if native {
		key = "parameters"
	}
	value, exists := fields[key]
	if !exists {
		return params, nil
	}
	switch v := value.(type) {
	case map[string]any:
		params = v
	case string:
		if v != "" && (json.Unmarshal([]byte(v), &params) != nil || params == nil) {
			return nil, errors.New("invalid Veo parameters")
		}
	default:
		return nil, errors.New("invalid Veo parameters")
	}
	return params, nil
}

// applyVeoDuration defaults and normalizes params["durationSeconds"] from
// "duration"/"seconds" fields, defaulting to 8 seconds.
func applyVeoDuration(fields, params map[string]any) error {
	if _, exists := params["durationSeconds"]; !exists {
		params["durationSeconds"] = 8
		for _, key := range []string{"duration", "seconds"} {
			if value, exists := fields[key]; exists {
				params["durationSeconds"] = value
				break
			}
		}
	}
	seconds, err := integer(params["durationSeconds"])
	if err != nil {
		return err
	}
	params["durationSeconds"] = seconds
	return nil
}

// applyVeoResolution defaults and validates params["resolution"], deriving
// it (and aspectRatio) from the "size" field when not already set.
func applyVeoResolution(fields, params map[string]any) error {
	if _, exists := params["resolution"]; !exists {
		params["resolution"] = "720p"
		if value, exists := fields["size"]; exists {
			size, ok := value.(string)
			if !ok {
				return errors.New("invalid Veo size")
			}
			resolution, ratio, err := sizeParameters(size)
			if err != nil {
				return err
			}
			params["resolution"] = resolution
			if _, exists := params["aspectRatio"]; !exists {
				params["aspectRatio"] = ratio
			}
		}
	}
	resolution, ok := params["resolution"].(string)
	if !ok {
		return errors.New("invalid Veo resolution")
	}
	resolution = strings.ToLower(resolution)
	if resolution != "720p" && resolution != "1080p" && resolution != "4k" {
		return errors.New("invalid Veo resolution")
	}
	params["resolution"] = resolution
	return nil
}
