package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

// Content decodes inline results or fetches Gemini media. API keys are sent only
// to the configured Gemini host, never to third-party signed media URLs.
func Content(ctx context.Context, client *http.Client, target gateway.Target, location string) (*http.Response, error) {
	if strings.HasPrefix(location, "data:") {
		return InlineContent(location)
	}
	u, err := url.Parse(location)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid Veo media URL")
	}
	endpoint, err := endpoint(target, "")
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(endpoint)
	headers := make(http.Header)
	if strings.EqualFold(u.Host, base.Host) && u.Scheme == base.Scheme {
		headers.Set("X-Goog-Api-Key", target.Secret)
	}
	target.BaseURL = endpoint
	response, err := native.Media(ctx, client, target, u.String(), headers)
	if err != nil {
		return nil, errors.New("veo content transport failed")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		return nil, &native.Rejected{Status: response.StatusCode}
	}
	return response, nil
}

// InlineContent returns decoded bytes without an additional upstream request.
func InlineContent(location string) (*http.Response, error) {
	meta, encoded, ok := strings.Cut(strings.TrimPrefix(location, "data:"), ",")
	if !strings.HasPrefix(location, "data:") || !ok || !strings.HasSuffix(meta, ";base64") {
		return nil, errors.New("invalid inline video")
	}
	kind := strings.TrimSuffix(meta, ";base64")
	if !strings.HasPrefix(kind, "video/") {
		return nil, errors.New("invalid inline video MIME type")
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(64<<20) {
		return nil, errors.New("inline video exceeds limit")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return nil, errors.New("invalid inline video base64")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {kind}}, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data))}, nil
}
