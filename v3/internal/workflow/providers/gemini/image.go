package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

const maxImageBytes = 20 << 20

func readFields(input native.Submit) (map[string]any, map[string]string, error) {
	var fields map[string]any
	media, params, err := mime.ParseMediaType(input.ContentType)
	if input.ContentType != "" && err != nil {
		return nil, nil, errors.New("invalid Veo content type")
	}
	if media != "multipart/form-data" {
		if json.Unmarshal(input.Body, &fields) != nil || fields == nil {
			return nil, nil, errors.New("invalid Veo JSON request")
		}
		return fields, nil, nil
	}
	if params["boundary"] == "" {
		return nil, nil, errors.New("missing Veo multipart boundary")
	}
	fields = make(map[string]any)
	var image map[string]string
	reader := multipart.NewReader(bytes.NewReader(input.Body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, errors.New("invalid Veo multipart request")
		}
		limit := 1 << 20
		if part.FileName() != "" {
			limit = maxImageBytes
		}
		value, err := io.ReadAll(io.LimitReader(part, int64(limit)+1))
		closeErr := part.Close()
		if err != nil || closeErr != nil || len(value) > limit {
			return nil, nil, errors.New("veo multipart part exceeds limit")
		}
		if part.FileName() == "" {
			fields[part.FormName()] = string(value)
		} else if part.FormName() == "input_reference" && image == nil {
			kind := part.Header.Get("Content-Type")
			if kind == "" || kind == "application/octet-stream" {
				kind = http.DetectContentType(value)
			}
			if !strings.HasPrefix(kind, "image/") || len(value) == 0 {
				return nil, nil, errors.New("veo input_reference must be an image")
			}
			image = map[string]string{"mimeType": kind, "bytesBase64Encoded": base64.StdEncoding.EncodeToString(value)}
		}
	}
	return fields, image, nil
}

func parseImage(value string) (map[string]string, error) {
	encoded, kind := strings.TrimSpace(value), ""
	if strings.HasPrefix(encoded, "data:") {
		meta, payload, ok := strings.Cut(strings.TrimPrefix(encoded, "data:"), ",")
		if !ok || !strings.HasSuffix(meta, ";base64") {
			return nil, errors.New("veo image requires a base64 data URI")
		}
		kind = strings.TrimSuffix(meta, ";base64")
		encoded = payload
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(maxImageBytes) {
		return nil, errors.New("veo image exceeds limit")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 {
		return nil, errors.New("invalid Veo image base64")
	}
	if kind == "" {
		kind = http.DetectContentType(raw)
	}
	if !strings.HasPrefix(kind, "image/") {
		return nil, errors.New("veo input must be an image")
	}
	return map[string]string{"mimeType": kind, "bytesBase64Encoded": encoded}, nil
}
