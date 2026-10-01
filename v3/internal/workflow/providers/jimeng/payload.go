package jimeng

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

const maxImageBytes = 4*1024*1024 + 700*1024

func requestBody(input native.Submit, upstream string) ([]byte, string, int, error) {
	fields, files, err := inputFields(input)
	if err != nil {
		return nil, "", 0, err
	}
	if err := mergeMetadataFields(fields); err != nil {
		return nil, "", 0, err
	}
	key, err := resolveReqKey(fields, upstream, input.Model)
	if err != nil {
		return nil, "", 0, err
	}
	frames, err := resolveFrames(fields)
	if err != nil {
		return nil, "", 0, err
	}
	images, err := resolveImages(fields, files)
	if err != nil {
		return nil, "", 0, err
	}
	if err := applyImages(fields, images); err != nil {
		return nil, "", 0, err
	}
	imageCount, err := validateImageLists(fields, len(images))
	if err != nil {
		return nil, "", 0, err
	}
	key = transformReqKey(key, imageCount)
	body, err := finalizeFields(fields, key, frames)
	return body, key, frames, err
}

// mergeMetadataFields unpacks the "metadata" field, which may itself be a
// JSON-encoded string, and merges its contents into fields.
func mergeMetadataFields(fields map[string]json.RawMessage) error {
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
		return errors.New("invalid jimeng metadata")
	}
	for k, v := range extra {
		fields[k] = v
	}
	return nil
}

// resolveReqKey picks the upstream model key from the explicit upstream
// override, the submitted model name, or the request's own req_key field.
func resolveReqKey(fields map[string]json.RawMessage, upstream, model string) (string, error) {
	key := upstream
	if key == "" {
		key = model
	}
	if key == "" {
		_ = json.Unmarshal(fields["req_key"], &key)
	}
	if key == "" {
		return "", errors.New("jimeng model required")
	}
	return key, nil
}

// resolveFrames derives the frame count from an explicit "frames" field, or
// from a 5/10 second "duration"/"seconds" field, defaulting to 121.
func resolveFrames(fields map[string]json.RawMessage) (int, error) {
	if v := fields["frames"]; len(v) > 0 {
		frames, err := integer(v)
		if err != nil || frames < 2 {
			return 0, errors.New("invalid jimeng frames")
		}
		return frames, nil
	}
	duration := fields["duration"]
	if len(duration) == 0 {
		duration = fields["seconds"]
	}
	frames := 121
	if len(duration) > 0 {
		seconds, err := integer(duration)
		if err != nil || (seconds != 5 && seconds != 10) {
			return 0, errors.New("jimeng duration must be 5 or 10 seconds")
		}
		if seconds == 10 {
			frames = 241
		}
	}
	return frames, nil
}

// resolveImages gathers candidate images from uploaded multipart files, a
// JSON "images" array, or a single "image"/"input_reference" field, in that
// order of precedence.
func resolveImages(fields map[string]json.RawMessage, files []string) ([]string, error) {
	images := files
	if len(images) == 0 && len(fields["images"]) > 0 {
		if err := json.Unmarshal(fields["images"], &images); err != nil {
			return nil, errors.New("invalid jimeng images")
		}
	}
	if len(images) == 0 {
		for _, k := range []string{"image", "input_reference"} {
			var image string
			if raw := fields[k]; len(raw) > 0 && json.Unmarshal(raw, &image) != nil {
				return nil, errors.New("invalid jimeng image")
			}
			if image != "" {
				images = []string{image}
				break
			}
		}
	}
	return images, nil
}

// applyImages validates that images are consistently URLs or base64 binary
// data, decodes and size-checks binary entries, and writes the resulting
// list into the appropriate upstream field.
func applyImages(fields map[string]json.RawMessage, images []string) error {
	if len(images) == 0 {
		return nil
	}
	urls := strings.HasPrefix(images[0], "https://") || strings.HasPrefix(images[0], "http://")
	for i, image := range images {
		isURL := strings.HasPrefix(image, "https://") || strings.HasPrefix(image, "http://")
		if isURL != urls {
			return errors.New("jimeng images cannot mix URLs and binary data")
		}
		if !urls {
			if strings.HasPrefix(image, "data:") {
				_, image, _ = strings.Cut(image, ",")
			}
			decoded, err := base64.StdEncoding.DecodeString(image)
			if err != nil || len(decoded) == 0 || len(decoded) > maxImageBytes {
				return errors.New("invalid or oversized jimeng image")
			}
			images[i] = image
		}
	}
	field := "binary_data_base64"
	if urls {
		field = "image_urls"
	}
	fields[field], _ = json.Marshal(images)
	return nil
}

// validateImageLists re-validates the (possibly just-written) image list
// fields and returns the maximum image count seen across them, starting
// from imageCount.
func validateImageLists(fields map[string]json.RawMessage, imageCount int) (int, error) {
	for _, k := range []string{"binary_data_base64", "image_urls"} {
		var list []string
		if v := fields[k]; len(v) > 0 {
			if err := json.Unmarshal(v, &list); err != nil {
				return 0, errors.New("invalid jimeng image list")
			}
			if len(list) > imageCount {
				imageCount = len(list)
			}
			if k == "binary_data_base64" {
				for _, image := range list {
					if len(image) > base64.StdEncoding.EncodedLen(maxImageBytes) {
						return 0, errors.New("oversized jimeng image")
					}
					decoded, err := base64.StdEncoding.DecodeString(image)
					if err != nil || len(decoded) == 0 || len(decoded) > maxImageBytes {
						return 0, errors.New("invalid or oversized jimeng image")
					}
				}
			}
		}
	}
	return imageCount, nil
}

// transformReqKey maps a generic jimeng_v30 key to the specific upstream
// variant based on the resolved image count.
func transformReqKey(key string, imageCount int) string {
	if !strings.Contains(key, "jimeng_v30") {
		return key
	}
	switch {
	case key == "jimeng_v30_pro":
		return "jimeng_ti2v_v30_pro"
	case imageCount > 1:
		return strings.TrimSuffix(strings.Replace(key, "jimeng_v30", "jimeng_i2v_first_tail_v30", 1), "p")
	case imageCount == 1:
		return strings.TrimSuffix(strings.Replace(key, "jimeng_v30", "jimeng_i2v_first_v30", 1), "p")
	default:
		return strings.Replace(key, "jimeng_v30", "jimeng_t2v_v30", 1)
	}
}

// finalizeFields sets req_key/frames/seed/aspect_ratio, strips fields that
// are not part of the upstream payload, and marshals the result.
func finalizeFields(fields map[string]json.RawMessage, key string, frames int) ([]byte, error) {
	fields["req_key"], _ = json.Marshal(key)
	fields["frames"], _ = json.Marshal(frames)
	if raw := fields["seed"]; len(raw) > 0 {
		n, err := integer(raw)
		if err != nil {
			return nil, errors.New("invalid jimeng seed")
		}
		fields["seed"], _ = json.Marshal(n)
	} else {
		fields["seed"] = json.RawMessage(`0`)
	}
	if _, ok := fields["aspect_ratio"]; !ok {
		fields["aspect_ratio"] = json.RawMessage(`""`)
	}
	for _, k := range []string{"model", "metadata", "duration", "seconds", "images", "image", "input_reference", "size"} {
		delete(fields, k)
	}
	return json.Marshal(fields)
}

func inputFields(input native.Submit) (map[string]json.RawMessage, []string, error) {
	media, params, err := mime.ParseMediaType(input.ContentType)
	if input.ContentType != "" && err != nil {
		return nil, nil, errors.New("invalid jimeng content type")
	}
	var fields map[string]json.RawMessage
	if media != "multipart/form-data" {
		if err := json.Unmarshal(input.Body, &fields); err != nil || fields == nil {
			return nil, nil, errors.New("invalid jimeng request")
		}
		return fields, nil, nil
	}
	if params["boundary"] == "" {
		return nil, nil, errors.New("jimeng multipart boundary required")
	}
	fields = make(map[string]json.RawMessage)
	reader := multipart.NewReader(bytes.NewReader(input.Body), params["boundary"])
	var images []string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, errors.New("invalid jimeng multipart")
		}
		limit := int64(1 << 20)
		if part.FileName() != "" {
			limit = maxImageBytes
		}
		body, readErr := io.ReadAll(io.LimitReader(part, limit+1))
		closeErr := part.Close()
		if readErr != nil || closeErr != nil || int64(len(body)) > limit {
			return nil, nil, errors.New("invalid or oversized jimeng part")
		}
		if part.FileName() != "" {
			if part.FormName() != "input_reference" || len(body) == 0 {
				return nil, nil, errors.New("invalid jimeng input file")
			}
			images = append(images, base64.StdEncoding.EncodeToString(body))
		} else {
			fields[part.FormName()], _ = json.Marshal(string(body))
		}
	}
	return fields, images, nil
}

func integer(raw json.RawMessage) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, errors.New("invalid integer")
	}
	return strconv.Atoi(s)
}
