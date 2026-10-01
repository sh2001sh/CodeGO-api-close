// Package gemini adapts Google's generateContent API and OpenAI chat requests.
package gemini

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

const ID = "gemini"
const maxJSONBody = 64 << 20

type Provider struct {
	ImageTransport ImageTransport
}

func (p Provider) BuildRequest(ctx context.Context, req *gateway.Request, t gateway.Target) (*http.Request, error) {
	base, err := apiBase(t)
	if err != nil {
		return nil, err
	}
	body := req.Body
	switch req.Protocol {
	case gateway.ProtocolGemini:
	case gateway.ProtocolOpenAIChat:
		body, err = p.inlineRemoteImages(ctx, body, t)
		if err != nil {
			return nil, err
		}
		body, err = chatRequest(body)
		if err != nil {
			return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
				Code: "unsupported_conversion", Message: err.Error()}
		}
	default:
		return nil, fmt.Errorf("gemini: unsupported client protocol %d", req.Protocol)
	}
	model := req.Model
	if t.UpstreamModel != "" {
		model = t.UpstreamModel
	}
	model = strings.TrimPrefix(model, "models/")
	if model == "" || strings.ContainsAny(model, "/?#") {
		return nil, fmt.Errorf("gemini: invalid model name")
	}
	endpoint := base + "/models/" + url.PathEscape(model) + ":generateContent"
	if req.Stream {
		endpoint = base + "/models/" + url.PathEscape(model) + ":streamGenerateContent?alt=sse"
	}
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("X-Goog-Api-Key", t.Secret)
	if req.Stream {
		upstream.Header.Set("Accept", "text/event-stream")
	}
	return upstream, nil
}

func apiBase(target gateway.Target) (string, error) {
	version := "v1beta"
	if configured := target.Settings["api_version"]; configured != nil {
		value, ok := configured.(string)
		if !ok {
			return "", channelConfigError("API version must be a string")
		}
		if value != "" {
			if value != "v1" && value != "v1beta" && value != "v1alpha" {
				return "", channelConfigError("unsupported or unsafe API version")
			}
			version = value
		}
	}
	base := strings.TrimRight(target.BaseURL, "/")
	for _, explicit := range []string{"v1", "v1beta", "v1alpha"} {
		if strings.HasSuffix(base, "/"+explicit) {
			return base, nil
		}
	}
	return base + "/" + version, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	s := &responseStream{body: resp.Body, req: req, native: req.Protocol == gateway.ProtocolGemini}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		s.reader = sse.NewReader(resp.Body, 0)
	}
	s.chat = newChatState(req)
	return &gatedStream{source: s}
}
