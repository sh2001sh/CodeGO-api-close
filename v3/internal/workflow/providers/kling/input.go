package kling

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func requestFields(input native.Submit) (map[string]json.RawMessage, error) {
	media, params, err := mime.ParseMediaType(input.ContentType)
	if input.ContentType != "" && err != nil {
		return nil, errors.New("invalid kling content type")
	}
	if media != "multipart/form-data" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(input.Body, &fields); err != nil || fields == nil {
			return nil, errors.New("invalid kling request")
		}
		return fields, nil
	}
	if params["boundary"] == "" {
		return nil, errors.New("kling multipart boundary required")
	}
	return parseKlingMultipart(input.Body, params["boundary"])
}

// parseKlingMultipart reads a multipart kling request into fields, decoding
// file parts as base64-encoded "image"/"image_tail" entries and leaving
// non-file parts as their raw or string-wrapped JSON value.
func parseKlingMultipart(body []byte, boundary string) (map[string]json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("invalid kling multipart")
		}
		if err := readKlingPart(fields, part); err != nil {
			return nil, err
		}
	}
	return fields, nil
}

// readKlingPart reads a single multipart part and stores it into fields.
func readKlingPart(fields map[string]json.RawMessage, part *multipart.Part) error {
	limit := int64(1 << 20)
	if part.FileName() != "" {
		limit = 10 << 20
	}
	body, readErr := io.ReadAll(io.LimitReader(part, limit+1))
	closeErr := part.Close()
	if readErr != nil || closeErr != nil || int64(len(body)) > limit {
		return errors.New("invalid or oversized kling part")
	}
	field := part.FormName()
	if part.FileName() != "" {
		if field == "input_reference" {
			field = "image"
			if _, exists := fields[field]; exists {
				field = "image_tail"
			}
		}
		if (field != "image" && field != "image_tail") || len(body) == 0 {
			return errors.New("invalid kling input file")
		}
		if _, exists := fields[field]; exists {
			return errors.New("duplicate kling input file")
		}
		fields[field], _ = json.Marshal(base64.StdEncoding.EncodeToString(body))
		return nil
	}
	switch field {
	case "cfg_scale", "dynamic_masks", "camera_control", "images":
		if !json.Valid(body) {
			return errors.New("invalid kling form field")
		}
		fields[field] = append(json.RawMessage(nil), body...)
	default:
		fields[field], _ = json.Marshal(string(body))
	}
	return nil
}
