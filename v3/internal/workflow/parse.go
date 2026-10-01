package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func (h *Handler) parse(w http.ResponseWriter, r *http.Request) (native.Submit, []byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes))
	if err != nil {
		return native.Submit{}, nil, err
	}
	in := native.Submit{Body: body, ContentType: r.Header.Get("Content-Type"), Action: r.PathValue("action")}
	fields, err := parseFields(body, in.ContentType)
	if err != nil {
		return in, nil, err
	}
	resolveSubmitModel(&in, r.URL.Path, fields)
	resolveSubmitAction(&in, fields)
	if in.Model != "" {
		fields["model"] = in.Model
	}
	billingBody, err := json.Marshal(fields)
	return in, billingBody, err
}

// parseFields decodes a JSON or multipart/form-data submit body into a flat
// field map. Multipart file parts are skipped; non-file parts are kept as
// strings.
func parseFields(body []byte, contentType string) (map[string]any, error) {
	media, params, _ := mime.ParseMediaType(contentType)
	if media != "multipart/form-data" {
		var fields map[string]any
		if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
			return nil, errors.New("invalid JSON body")
		}
		return fields, nil
	}
	fields := make(map[string]any)
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("invalid multipart")
		}
		if part.FileName() == "" {
			value, err := io.ReadAll(io.LimitReader(part, 1<<20))
			if err != nil {
				return nil, err
			}
			fields[part.FormName()] = string(value)
		}
		if err = part.Close(); err != nil {
			return nil, err
		}
	}
	return fields, nil
}

// resolveSubmitModel resolves in.Model from the field aliases model/model_name/req_key,
// then applies route-family defaults (suno/kling).
func resolveSubmitModel(in *native.Submit, path string, fields map[string]any) {
	in.Model, _ = fields["model"].(string)
	if in.Model == "" {
		in.Model, _ = fields["model_name"].(string)
	}
	if in.Model == "" {
		in.Model, _ = fields["req_key"].(string)
	}
	if strings.HasPrefix(path, "/suno/") {
		if strings.EqualFold(in.Action, "lyrics") {
			in.Model = "suno_lyrics"
		} else {
			in.Model = "suno_music"
		}
	} else if strings.HasPrefix(path, "/kling/") && in.Model == "" {
		in.Model = "kling-v1"
	}
}

// resolveSubmitAction defaults in.Action to "textGenerate", or "generate"
// when an image field is present.
func resolveSubmitAction(in *native.Submit, fields map[string]any) {
	if in.Action == "" {
		in.Action = "textGenerate"
	}
	if image, ok := fields["image"]; ok && image != "" {
		in.Action = "generate"
	}
}

func pricingHeaders(r *http.Request) map[string]string {
	headers := make(map[string]string, len(r.Header)+1)
	for key, values := range r.Header {
		switch strings.ToLower(key) {
		case "authorization", "x-api-key", "x-goog-api-key", "cookie", "proxy-authorization":
			continue
		}
		headers[key] = strings.Join(values, ",")
	}
	headers["X-Codego-Operation"] = "video/generations"
	if strings.HasPrefix(r.URL.Path, "/suno/") {
		headers["X-Codego-Operation"] = "suno/submit"
	}
	return headers
}
