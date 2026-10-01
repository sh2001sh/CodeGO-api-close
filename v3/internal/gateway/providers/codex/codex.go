// Package codex adapts the ChatGPT Codex Responses API.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

const ID = "codex"

type Provider struct{}

type credential struct {
	AccessToken string `json:"access_token"`
	AccountID   string `json:"account_id"`
}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolResponses && req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "unsupported_protocol",
			Message: fmt.Sprintf("codex: unsupported client protocol %d", req.Protocol)}
	}
	var key credential
	if err := json.Unmarshal([]byte(target.Secret), &key); err != nil {
		return nil, errors.New("codex: credential must be a JSON object")
	}
	key.AccessToken, key.AccountID = strings.TrimSpace(key.AccessToken), strings.TrimSpace(key.AccountID)
	if key.AccessToken == "" || key.AccountID == "" {
		return nil, errors.New("codex: access_token and account_id are required")
	}
	endpoint, err := codexURL(target.BaseURL)
	if err != nil {
		return nil, err
	}
	canonical := target
	canonical.BaseURL = "https://codex.invalid"
	out, err := (responses.Provider{}).BuildRequest(ctx, req, canonical)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(out.Body).Decode(&body); err != nil || body == nil {
		_ = out.Body.Close()
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "invalid_request",
			Message: "codex: body must be a JSON object"}
	}
	_ = out.Body.Close()
	body["store"] = json.RawMessage("false")
	if instructions, ok := body["instructions"]; !ok || string(instructions) == "null" {
		body["instructions"] = json.RawMessage(`""`)
	}
	for _, field := range []string{"max_output_tokens", "max_tokens", "temperature", "top_p", "frequency_penalty", "presence_penalty"} {
		delete(body, field)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	out, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Authorization", "Bearer "+key.AccessToken)
	out.Header.Set("Chatgpt-Account-Id", key.AccountID)
	out.Header.Set("Openai-Beta", "responses=experimental")
	out.Header.Set("Originator", "codex_cli_rs")
	out.Header.Set("Content-Type", "application/json")
	if req.Stream {
		out.Header.Set("Accept", "text/event-stream")
	} else {
		out.Header.Set("Accept", "application/json")
	}
	return out, nil
}

func codexURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("codex: base URL must be an absolute HTTP URL")
	}
	path := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(path, "/backend-api/codex/responses"):
	case strings.HasSuffix(path, "/backend-api/codex"):
		path += "/responses"
	case strings.HasSuffix(path, "/backend-api"):
		path += "/codex/responses"
	default:
		path = strings.TrimSuffix(path, "/v1") + "/backend-api/codex/responses"
	}
	u.Path, u.RawPath = path, ""
	return u.String(), nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return (responses.Provider{}).Decode(req, resp)
}
