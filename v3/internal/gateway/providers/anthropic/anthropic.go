// Package anthropic adapts Messages upstreams and translates OpenAI Chat clients.
package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/sjson"
)

const ID = "anthropic"
const maxJSONBody = 64 << 20

type Provider struct{}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	body := req.Body
	model := req.Model
	if target.UpstreamModel != "" {
		model = target.UpstreamModel
	}
	var err error
	switch req.Protocol {
	case gateway.ProtocolAnthropic:
		if model != req.Model {
			body, err = sjson.SetBytes(body, "model", model)
		}
	case gateway.ProtocolOpenAIChat:
		body, err = convertRequest(body, model)
		if err != nil {
			return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
				Code: "unsupported_request", Message: err.Error()}
		}
	default:
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
			Code: "unsupported_protocol", Message: fmt.Sprintf("anthropic: unsupported client protocol %d", req.Protocol)}
	}
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(target.BaseURL, "/")
	base = strings.TrimSuffix(base, "/v1")
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("X-Api-Key", target.Secret)
	out.Header.Set("Anthropic-Version", "2023-06-01")
	if req.Stream {
		out.Header.Set("Accept", "text/event-stream")
	}
	return out, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	chat := req.Protocol == gateway.ProtocolOpenAIChat
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return &gatedStream{source: &stream{body: resp.Body, reader: sse.NewReader(resp.Body, 0), chat: chat,
			model: req.Model, created: req.Received.Unix(), wantUsage: clientWantsUsage(req.Body)}}
	}
	return &single{body: resp.Body, chat: chat, model: req.Model, created: req.Received.Unix()}
}

func readBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxJSONBody+1))
	if err == nil && len(data) > maxJSONBody {
		err = fmt.Errorf("anthropic: response exceeds %d bytes", maxJSONBody)
	}
	return data, err
}
