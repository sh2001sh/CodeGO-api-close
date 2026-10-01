package ali

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func payload(input native.Submit, upstream string) ([]byte, error) {
	fields, err := requestFields(input)
	if err != nil {
		return nil, err
	}
	model, err := resolveAliModel(fields, input.Model, upstream)
	if err != nil {
		return nil, err
	}
	if _, ok := fields["input"].(map[string]any); ok {
		fields["model"] = model
		return json.Marshal(fields)
	}
	in := aliInput(fields)
	params, err := aliParams(fields, model)
	if err != nil {
		return nil, err
	}
	if err := applyAliMetadata(fields, model, in, params); err != nil {
		return nil, err
	}
	if _, err := positiveInt(params["duration"]); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"model": model, "input": in, "parameters": params})
}

// resolveAliModel picks the upstream model from the explicit upstream
// override, the submitted model name, or the request's own "model" field.
func resolveAliModel(fields map[string]any, inputModel, upstream string) (string, error) {
	model := inputModel
	if upstream != "" {
		model = upstream
	}
	if model == "" {
		model, _ = fields["model"].(string)
	}
	if model == "" {
		return "", errors.New("ali model required")
	}
	return model, nil
}

// aliInput builds the "input" object from the prompt and optional image
// reference.
func aliInput(fields map[string]any) map[string]any {
	in := map[string]any{"prompt": fields["prompt"]}
	if reference, _ := fields["input_reference"].(string); reference != "" {
		in["img_url"] = reference
	}
	return in
}

// aliParams builds the default "parameters" object, resolving size/resolution
// and duration from fields.
func aliParams(fields map[string]any, model string) (map[string]any, error) {
	params := map[string]any{"prompt_extend": true, "watermark": false, "duration": 5}
	if size, _ := fields["size"].(string); size != "" {
		if strings.ContainsAny(size, "*x") {
			params["size"] = strings.ReplaceAll(size, "x", "*")
		} else {
			if strings.Contains(model, "t2v") {
				return nil, errors.New("ali text-to-video size requires width*height")
			}
			params["resolution"] = strings.TrimSuffix(strings.ToUpper(size), "P") + "P"
		}
	} else if strings.Contains(model, "t2v") {
		params["size"] = "1280*720"
		if strings.HasPrefix(model, "wan2.5") || strings.HasPrefix(model, "wan2.2") {
			params["size"] = "1920*1080"
		}
	} else {
		params["resolution"] = "720P"
		if strings.HasPrefix(model, "wan2.6") || strings.HasPrefix(model, "wan2.5") || strings.HasPrefix(model, "wan2.2-i2v-plus") {
			params["resolution"] = "1080P"
		}
	}
	for _, key := range []string{"seconds", "duration"} {
		if value, ok := fields[key]; ok {
			duration, err := positiveInt(value)
			if err != nil {
				return nil, err
			}
			params["duration"] = duration
		}
	}
	return params, nil
}

// applyAliMetadata unpacks "metadata" (which may be a JSON-encoded string)
// and merges its "input"/"parameters" overrides into in/params, rejecting
// any attempt to change the model.
func applyAliMetadata(fields map[string]any, model string, in, params map[string]any) error {
	metadata, ok := fields["metadata"].(map[string]any)
	if encoded, isString := fields["metadata"].(string); isString && encoded != "" {
		if json.Unmarshal([]byte(encoded), &metadata) != nil {
			return errors.New("invalid Ali metadata")
		}
		ok = true
	}
	if !ok {
		return nil
	}
	if altered, exists := metadata["model"]; exists && altered != model {
		return errors.New("metadata cannot change Ali model")
	}
	if overrides, exists := metadata["input"].(map[string]any); exists {
		for k, v := range overrides {
			in[k] = v
		}
	}
	if overrides, exists := metadata["parameters"].(map[string]any); exists {
		for k, v := range overrides {
			params[k] = v
		}
	}
	return nil
}

func positiveInt(value any) (int, error) {
	var n int
	switch v := value.(type) {
	case float64:
		if v != float64(int(v)) {
			return 0, errors.New("invalid Ali duration")
		}
		n = int(v)
	case int:
		n = v
	case string:
		var err error
		n, err = strconv.Atoi(v)
		if err != nil {
			return 0, errors.New("invalid Ali duration")
		}
	default:
		return 0, errors.New("invalid Ali duration")
	}
	if n <= 0 || n > 3600 {
		return 0, errors.New("invalid Ali duration")
	}
	return n, nil
}

func requestFields(input native.Submit) (map[string]any, error) {
	var fields map[string]any
	media, params, err := mime.ParseMediaType(input.ContentType)
	if input.ContentType != "" && err != nil {
		return nil, errors.New("invalid Ali content type")
	}
	if media != "multipart/form-data" {
		if json.Unmarshal(input.Body, &fields) != nil || fields == nil {
			return nil, errors.New("invalid Ali JSON request")
		}
		return fields, nil
	}
	if params["boundary"] == "" {
		return nil, errors.New("missing Ali multipart boundary")
	}
	fields = make(map[string]any)
	reader := multipart.NewReader(bytes.NewReader(input.Body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("invalid Ali multipart request")
		}
		if part.FileName() != "" {
			_ = part.Close()
			return nil, errors.New("ali image input requires an image URL")
		}
		value, err := io.ReadAll(io.LimitReader(part, (1<<20)+1))
		closeErr := part.Close()
		if err != nil || closeErr != nil || len(value) > 1<<20 {
			return nil, errors.New("invalid Ali multipart field")
		}
		fields[part.FormName()] = string(value)
	}
	return fields, nil
}
