package auxiliary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func unsupported(operation Operation) error {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "unsupported_operation", Message: "channel does not support " + string(operation)}
}

func upstreamModel(req *gateway.Request, target gateway.Target) string {
	if target.UpstreamModel != "" {
		return target.UpstreamModel
	}
	return strings.TrimSuffix(req.Model, "-openai-compact")
}

func endpoint(base, path string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("auxiliary: invalid channel URL")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	u.RawPath = ""
	u.Fragment = ""
	return u.String(), nil
}

func jsonRequest(ctx context.Context, address, secret string, body any) (*http.Request, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	if secret != "" {
		r.Header.Set("Authorization", "Bearer "+secret)
	}
	return r, nil
}

func object(body []byte) (map[string]json.RawMessage, error) {
	var data map[string]json.RawMessage
	if err := json.Unmarshal(body, &data); err != nil || data == nil {
		return nil, errors.New("auxiliary: request must be an object")
	}
	return data, nil
}

func readResponse(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	const maximum = 64 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maximum+1))
	if err == nil && len(data) > maximum {
		return nil, errors.New("auxiliary: response too large")
	}
	return data, err
}

func rawResponse(resp *http.Response) (Response, error) {
	data, err := readResponse(resp)
	return Response{Body: data, Header: resp.Header.Clone(), Usage: parseUsage(data)}, err
}
