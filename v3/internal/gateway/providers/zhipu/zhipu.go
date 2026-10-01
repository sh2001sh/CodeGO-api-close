// Package zhipu adapts the legacy native Zhipu v3 Chat API.
package zhipu

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const ID = "zhipu"
const maxBody = 64 << 20
const maxEvent = 8 << 20

type Provider struct{}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, invalid("unsupported_protocol", "zhipu: unsupported client protocol")
	}
	body, err := convertRequest(req.Body, req.Stream)
	if err != nil {
		return nil, invalid("unsupported_request", err.Error())
	}
	token, err := signCredential(target.Secret, time.Now())
	if err != nil {
		return nil, err
	}
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	if model == "" || strings.ContainsAny(model, "/?#\\%") {
		return nil, invalid("unsupported_request", "zhipu: invalid model name")
	}
	method := "invoke"
	if req.Stream {
		method = "sse-invoke"
	}
	base := strings.TrimRight(target.BaseURL, "/")
	base = strings.TrimSuffix(base, "/api/paas/v3")
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/paas/v3/model-api/"+model+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Authorization", token)
	if req.Stream {
		out.Header.Set("Accept", "text/event-stream")
	}
	return out, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return newResponseStream(req, resp)
}

func signCredential(secret string, now time.Time) (string, error) {
	parts := strings.Split(secret, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "invalid_credential", Message: "zhipu: credential must contain an id and signing secret"}
	}
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT", "sign_type": "SIGN"})
	claims, _ := json.Marshal(map[string]any{"api_key": parts[0], "exp": now.Add(24 * time.Hour).UnixMilli(), "timestamp": now.UnixMilli()})
	encode := base64.RawURLEncoding.EncodeToString
	data := encode(header) + "." + encode(claims)
	mac := hmac.New(sha256.New, []byte(parts[1]))
	_, _ = mac.Write([]byte(data))
	return data + "." + encode(mac.Sum(nil)), nil
}

func invalid(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: code, Message: message}
}

func upstream(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}
