// Package coze adapts Coze's native bot Chat API to OpenAI Chat clients.
package coze

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

const ID = "coze"
const maxResponseSize = 16 << 20

type Provider struct{}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req == nil || req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, invalid("unsupported_protocol", "Coze supports Chat requests only")
	}
	bot, token, err := credentials(target.Secret)
	if err != nil {
		return nil, err
	}
	body, conversation, err := convertRequest(req, bot)
	if err != nil {
		return nil, invalid("unsupported_request", err.Error())
	}
	base := target.BaseURL
	if base == "" {
		base = "https://api.coze.com"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return nil, upstream("invalid_base_url", "Coze requires an absolute HTTP base URL")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/v3/chat") {
		u.Path = strings.TrimSuffix(u.Path, "/v3") + "/v3/chat"
	}
	u.RawPath = ""
	query := u.Query()
	if conversation != "" {
		query.Set("conversation_id", conversation)
	}
	u.RawQuery = query.Encode()
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Authorization", "Bearer "+token)
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Accept", "text/event-stream")
	return out, nil
}

// Coze's blocking API creates an asynchronous chat and requires further polling.
// Always request its native stream instead: all I/O then stays on the gateway's
// selected transport and deadline, including for blocking client requests.
func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	s := &responseStream{req: req, body: resp.Body, reader: sse.NewReader(resp.Body, maxResponseSize),
		messages: make(map[string]messageState), wantUsage: gjson.GetBytes(req.Body, "stream_options.include_usage").Bool()}
	if resp.Request != nil {
		s.ctx = resp.Request.Context()
	}
	s.jsonResponse = strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json")
	return s
}

func credentials(secret string) (string, string, error) {
	var bot, token string
	if strings.HasPrefix(strings.TrimSpace(secret), "{") {
		var in struct {
			BotID       string `json:"bot_id"`
			Token       string `json:"token"`
			AccessToken string `json:"access_token"`
			APIKey      string `json:"api_key"`
		}
		if json.Unmarshal([]byte(secret), &in) != nil {
			return "", "", upstream("invalid_credentials", "Coze credentials require bot_id and token")
		}
		bot, token = in.BotID, in.Token
		if token == "" {
			token = in.AccessToken
		}
		if token == "" {
			token = in.APIKey
		}
	} else {
		bot, token, _ = strings.Cut(secret, "|")
	}
	if strings.TrimSpace(bot) == "" || strings.TrimSpace(token) == "" || strings.ContainsAny(bot+token, "\r\n") || strings.Contains(token, "|") {
		return "", "", upstream("invalid_credentials", "Coze credentials require bot_id|token or a JSON bot_id and token")
	}
	return bot, token, nil
}

func invalid(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: code, Message: message}
}

func upstream(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}
