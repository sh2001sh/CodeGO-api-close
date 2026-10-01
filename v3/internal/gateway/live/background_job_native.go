package live

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

func (h *Handler) pollNativeBackground(ctx context.Context, job *BackgroundJob, req *gateway.Request, target gateway.Target, cancelRequested bool) error {
	method := http.MethodGet
	query := url.Values{"stream": {"true"}, "starting_after": {strconv.FormatInt(job.LastUpstreamSequence, 10)}}
	if cancelRequested {
		method, query = http.MethodPost, url.Values{}
	}
	upstream, err := backgroundRequest(ctx, req, target, job.UpstreamID, method, query)
	if err != nil {
		return err
	}
	client, err := h.upstreamClient(ctx, target)
	if err != nil {
		if upstream.Body != nil {
			_ = upstream.Body.Close()
		}
		return err
	}
	resp, err := client.Do(upstream)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	resp.StatusCode = gateway.MapUpstreamStatus(resp.StatusCode, target)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("live: native background poll rejected")
	}
	return h.consumeNativeBackground(ctx, job, req, resp)
}

func (h *Handler) consumeNativeBackground(ctx context.Context, job *BackgroundJob, req *gateway.Request, resp *http.Response) error {
	if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		body, err := io.ReadAll(io.LimitReader(resp.Body, h.cfg.MaxBodyBytes+1))
		if err != nil || int64(len(body)) > h.cfg.MaxBodyBytes || !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
			if job.UpstreamID == "" {
				return h.markBackgroundUnknown(context.WithoutCancel(ctx), job)
			}
			return errors.New("live: invalid background snapshot")
		}
		return h.persistBackgroundEvent(ctx, job, req, "", body)
	}
	reader := sse.NewReader(resp.Body, int(min(h.cfg.MaxBodyBytes, sse.DefaultMaxEventSize)))
	for {
		event, err := reader.Next()
		if err != nil {
			if job.UpstreamID == "" {
				return h.markBackgroundUnknown(context.WithoutCancel(ctx), job)
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if len(event.Data) == 0 {
			continue
		}
		if err := h.persistBackgroundEvent(ctx, job, req, string(event.Name), event.Data); err != nil {
			return err
		}
		if backgroundTerminal(job.Status) {
			return nil
		}
	}
}
