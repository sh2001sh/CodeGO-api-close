package auxiliary

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func uploadReplicateMedia(ctx context.Context, target gateway.Target, in Input, req *gateway.Request) (string, error) {
	form, contentType, err := buildReplicateUploadForm(in)
	if err != nil {
		return "", err
	}
	r, err := newReplicateUploadRequest(ctx, target, req, form, contentType)
	if err != nil {
		return "", err
	}
	return sendReplicateUpload(ctx, target, r)
}

// buildReplicateUploadForm re-encodes the first image part of the client's
// multipart upload into a single-field "content" multipart form suitable
// for the Replicate files API, returning the form body and its content type.
func buildReplicateUploadForm(in Input) (*bytes.Buffer, string, error) {
	mediaType, params, err := mime.ParseMediaType(in.ContentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, "", failure(400, "invalid_multipart", "image edit requires multipart images")
	}
	reader := multipart.NewReader(bytes.NewReader(in.Body), params["boundary"])
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	found := false
	for count := 0; count < 128; count++ {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", failure(400, "invalid_multipart", "invalid image edit form")
		}
		if part.FileName() == "" || (part.FormName() != "image" && part.FormName() != "image[]" && part.FormName() != "image_prompt") {
			continue
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "content", "filename": part.FileName()}))
		contentType := part.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		header.Set("Content-Type", contentType)
		content, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", failure(400, "invalid_multipart", "invalid image filename")
		}
		size, err := io.Copy(content, io.LimitReader(part, (32<<20)+1))
		if err != nil || size == 0 || size > 32<<20 {
			return nil, "", failure(400, "invalid_image", "invalid image file")
		}
		found = true
		break
	}
	if !found {
		return nil, "", failure(400, "missing_image", "image edit requires an image file")
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &form, writer.FormDataContentType(), nil
}

// newReplicateUploadRequest builds the outbound HTTP request for the
// Replicate files API, applying auth, the form content type, and (when a
// request context is present) the caller's upstream request overrides.
func newReplicateUploadRequest(ctx context.Context, target gateway.Target, req *gateway.Request, form *bytes.Buffer, contentType string) (*http.Request, error) {
	base := mediaBase(target, "https://api.replicate.com")
	base = strings.TrimSuffix(base, "/v1")
	address, err := endpoint(base, "/v1/files")
	if err != nil {
		return nil, mediaError("replicate", "invalid_endpoint")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, address, form)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+target.Secret)
	r.Header.Set("Content-Type", contentType)
	if req != nil {
		headers := target
		headers.ParamOverride, headers.Settings, headers.StatusCodeMapping = nil, nil, nil
		if err := gateway.ApplyUpstreamRequest(r, req, headers); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// sendReplicateUpload performs the upload request and extracts the signed
// "get" URL from the Replicate files API response.
func sendReplicateUpload(ctx context.Context, target gateway.Target, r *http.Request) (string, error) {
	client, err := (&mediaAdapter{provider: "replicate"}).httpClient(ctx, target)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", mediaError("replicate", "file_upload_failed")
	}
	data, err := readResponse(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", mediaError("replicate", "file_upload_failed")
	}
	if !gjson.ValidBytes(data) {
		return "", mediaError("replicate", "invalid_upload_response")
	}
	value := gjson.GetBytes(data, "urls.get").String()
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", mediaError("replicate", "invalid_upload_response")
	}
	return value, nil
}
