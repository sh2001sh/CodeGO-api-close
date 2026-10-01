// Package responses adapts OpenAI Responses upstreams without changing their
// lifecycle event order or discarding terminal usage.
package responses

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/sjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

const ID = "responses"

const maxJSONBody = 64 << 20

type Provider struct{}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolResponses && req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, errors.New("responses: unsupported client protocol")
	}
	body := req.Body
	if req.Protocol == gateway.ProtocolOpenAIChat {
		var err error
		body, err = chatRequestBody(req)
		if err != nil {
			return nil, err
		}
	}
	if target.UpstreamModel != "" && target.UpstreamModel != req.Model {
		var err error
		body, err = sjson.SetBytes(body, "model", target.UpstreamModel)
		if err != nil {
			return nil, err
		}
	}
	base := strings.TrimRight(target.BaseURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+target.Secret)
	if req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	return httpReq, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	var source gateway.EventStream
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		s := &stream{body: resp.Body, reader: sse.NewReader(resp.Body, 0), tools: newToolMeter(req)}
		if !req.Stream {
			source = &collected{source: s}
		} else {
			source = s
		}
	} else {
		source = &single{body: resp.Body, tools: newToolMeter(req)}
	}
	if req.Protocol == gateway.ProtocolOpenAIChat {
		return newChatStream(req, source)
	}
	return source
}

type single struct {
	body  io.ReadCloser
	done  bool
	tools toolMeter
}

func (s *single) Next() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	s.done = true
	body, err := io.ReadAll(io.LimitReader(s.body, maxJSONBody+1))
	if err != nil {
		return gateway.Event{}, err
	}
	if len(body) > maxJSONBody {
		return gateway.Event{}, sse.ErrEventTooLarge
	}
	root, failure := parseBody(body)
	if failure != nil {
		return gateway.Event{Kind: gateway.EventError, Err: failure}, nil
	}
	u := s.tools.apply(root, parseUsage(root.Get("usage")))
	if failure = responseError(root); failure != nil {
		return gateway.Event{Kind: gateway.EventError, Err: failure, Usage: u}, nil
	}
	if !hasOutput(root.Get("output")) {
		return gateway.Event{Kind: gateway.EventError, Err: emptyError(), Usage: u}, nil
	}
	return gateway.Event{Kind: gateway.EventData, Payload: body, Usage: u, TextBytes: outputTextBytes(root.Get("output"))}, nil
}

func (s *single) Close() error { return s.body.Close() }
