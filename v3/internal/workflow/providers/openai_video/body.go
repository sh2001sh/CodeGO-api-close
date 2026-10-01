package openai_video

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func requestBody(input native.Submit, model string) ([]byte, string, error) {
	if model == "" {
		return nil, "", errors.New("missing upstream model")
	}
	contentType := input.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", errors.New("invalid video content type")
	}
	if mediaType == "application/json" {
		var body map[string]json.RawMessage
		if json.Unmarshal(input.Body, &body) != nil || body == nil {
			return nil, "", errors.New("invalid video JSON request")
		}
		body["model"], _ = json.Marshal(model)
		data, err := json.Marshal(body)
		return data, contentType, err
	}
	if mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, "", errors.New("video requires JSON or multipart")
	}
	reader := multipart.NewReader(bytes.NewReader(input.Body), params["boundary"])
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", model); err != nil {
		return nil, "", err
	}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", errors.New("invalid video multipart request")
		}
		if part.FormName() == "model" {
			_ = part.Close()
			continue
		}
		output, err := writer.CreatePart(part.Header)
		if err != nil {
			_ = part.Close()
			return nil, "", errors.New("invalid video multipart part")
		}
		_, err = io.Copy(output, part)
		_ = part.Close()
		if err != nil {
			return nil, "", errors.New("video multipart read failed")
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}
