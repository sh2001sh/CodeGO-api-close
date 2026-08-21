package capability

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

var errInvalidBaseURL = errors.New("invalid base URL")

type responseEnvelope struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Code    any    `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func probePlainResponses(ctx context.Context, client *http.Client, endpoint, key, model string) (int, string) {
	body := map[string]any{"model": model, "input": "Reply with OK.", "max_output_tokens": 16}
	status, raw, err := requestJSON(ctx, client, http.MethodPost, endpoint, key, body)
	if err != nil {
		return status, classifyTransportError(err)
	}
	if status < 200 || status >= 300 {
		return status, responseErrorClass(raw, status)
	}
	return status, ""
}

func requestJSON(ctx context.Context, client *http.Client, method, target, key string, body any) (int, []byte, error) {
	response, err := request(ctx, client, method, target, key, body, "application/json, text/event-stream")
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	return response.StatusCode, raw, err
}

func request(ctx context.Context, client *http.Client, method, target, key string, body any, accept string) (*http.Response, error) {
	return requestWithHeaders(ctx, client, method, target, key, body, accept, nil)
}

func requestWithHeaders(ctx context.Context, client *http.Client, method, target, key string, body any, accept string, headers http.Header) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		raw, err := platformencoding.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "new-api-capability-probe/1")
	for name, values := range headers {
		req.Header.Del(name)
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	return client.Do(req)
}

func readProbeBody(response *http.Response) ([]byte, error) {
	if response == nil || response.Body == nil {
		return nil, nil
	}
	return io.ReadAll(io.LimitReader(response.Body, 2<<20))
}

func responseErrorClass(body []byte, status int) string {
	var response responseEnvelope
	if platformencoding.Unmarshal(body, &response) == nil && response.Error != nil {
		if value := strings.TrimSpace(fmt.Sprint(response.Error.Code)); value != "" && value != "<nil>" {
			return sanitizeClass(value)
		}
		if response.Error.Type != "" {
			return sanitizeClass(response.Error.Type)
		}
		return messageClass(response.Error.Message)
	}
	return "http_" + strconv.Itoa(status)
}

func classifyTransportError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	for _, class := range []string{"timeout", "connection reset", "unexpected eof", "handshake"} {
		if strings.Contains(text, class) {
			return strings.ReplaceAll(class, " ", "_")
		}
	}
	return "transport_error"
}

func messageClass(message string) string {
	message = strings.ToLower(message)
	for _, keyword := range []string{"background", "unsupported", "model", "authentication", "rate limit", "upstream", "timeout", "not found", "invalid"} {
		if strings.Contains(message, keyword) {
			return strings.ReplaceAll(keyword, " ", "_")
		}
	}
	return "api_error"
}

func sanitizeClass(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(" ", "_", "-", "_", ".", "_").Replace(value)
	if len(value) > 64 {
		return value[:64]
	}
	return value
}
