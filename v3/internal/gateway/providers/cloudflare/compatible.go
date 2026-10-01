package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

// ChatProvider preserves Cloudflare's account-scoped, OpenAI-compatible Chat
// API, including tools and images. Provider separately serves the native run API.
type ChatProvider struct{}

func (ChatProvider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, requestError("unsupported_protocol", "cloudflare: ChatProvider requires the Chat protocol")
	}
	native, err := compatibleTarget(target)
	if err != nil {
		return nil, err
	}
	return (openai.Provider{}).BuildRequest(ctx, req, native)
}

func (ChatProvider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return (openai.Provider{}).Decode(req, resp)
}

// ResponsesProvider preserves native response lifecycle events at Cloudflare's
// account-scoped Responses endpoint, without converting them to Chat.
type ResponsesProvider struct{}

func (ResponsesProvider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolResponses {
		return nil, requestError("unsupported_protocol", "cloudflare: ResponsesProvider requires the Responses protocol")
	}
	native, err := compatibleTarget(target)
	if err != nil {
		return nil, err
	}
	return (responses.Provider{}).BuildRequest(ctx, req, native)
}

func (ResponsesProvider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return (responses.Provider{}).Decode(req, resp)
}

func compatibleTarget(target gateway.Target) (gateway.Target, error) {
	var account, token string
	if strings.HasPrefix(strings.TrimSpace(target.Secret), "{") {
		var credentials struct {
			Account string `json:"account_id"`
			Token   string `json:"token"`
			APIKey  string `json:"api_key"`
		}
		if json.Unmarshal([]byte(target.Secret), &credentials) != nil {
			return target, configurationError("invalid_credentials", "cloudflare: invalid JSON credential")
		}
		account, token = credentials.Account, credentials.Token
		if token == "" {
			token = credentials.APIKey
		}
	} else if strings.Contains(target.Secret, "|") {
		account, token, _ = strings.Cut(target.Secret, "|")
	} else {
		token = target.Secret
	}
	if token == "" || strings.ContainsAny(token, "| \t\r\n") || (account != "" && !validSegment(account)) {
		return target, configurationError("invalid_credentials", "cloudflare: invalid account or token credential")
	}
	for _, c := range token {
		if c < 0x20 || c == 0x7f {
			return target, configurationError("invalid_credentials", "cloudflare: invalid token credential")
		}
	}
	base := strings.TrimRight(target.BaseURL, "/")
	if base == "" {
		base = "https://api.cloudflare.com"
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" {
		return target, configurationError("invalid_base_url", "cloudflare: base URL must be an HTTP URL without credentials, query or fragment")
	}
	path := strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/v1")
	if index := strings.LastIndex(path, "/accounts/"); index >= 0 {
		configured, tail, ok := strings.Cut(path[index+len("/accounts/"):], "/")
		if !ok || tail != "ai" || !validSegment(configured) {
			return target, configurationError("invalid_base_url", "cloudflare: account base URL must end with /accounts/{account}/ai")
		}
		if account != "" && account != configured {
			return target, configurationError("invalid_credentials", "cloudflare: configured account conflicts with credential account")
		}
	} else {
		if account == "" {
			return target, configurationError("invalid_credentials", "cloudflare: an account is required in credentials or base URL")
		}
		path = strings.TrimSuffix(path, "/client/v4") + "/client/v4/accounts/" + account + "/ai"
	}
	u.Path, u.RawPath = path, ""
	target.BaseURL, target.Secret = u.String(), token
	return target, nil
}

var _ gateway.Provider = ChatProvider{}
var _ gateway.Provider = ResponsesProvider{}
