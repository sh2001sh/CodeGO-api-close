package native

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

var ErrContentUnsupported = errors.New("provider does not expose task content")

// Rejected marks a definitive provider rejection, safe to refund. Transport
// failures and malformed successful responses may conceal upstream acceptance.
type Rejected struct{ Status int }

func (e *Rejected) Error() string { return fmt.Sprintf("task provider HTTP %d", e.Status) }

type InvalidRequest struct{ Err error }

func (e *InvalidRequest) Error() string { return e.Err.Error() }
func (e *InvalidRequest) Unwrap() error { return e.Err }

var transports = httpx.NewPool(httpx.TransportConfig{})

type proxyTransport struct{ fallback http.RoundTripper }

func (p proxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	policy, _ := req.Context().Value(requestContextKey{}).(requestPolicy)
	if policy.clients != nil {
		client, err := policy.clients(req.Context(), policy.target)
		if err != nil || client == nil {
			return nil, errors.New("task credential transport unavailable")
		}
		transport := client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		return transport.RoundTrip(req)
	}
	proxy := policy.target.ProxyURL
	if proxy == "" {
		return p.fallback.RoundTrip(req)
	}
	transport, err := transports.Transport(proxy)
	if err != nil {
		return nil, errors.New("invalid task channel proxy")
	}
	return transport.RoundTrip(req)
}

func Client(c *http.Client) *http.Client {
	var out http.Client
	if c != nil {
		out = *c
	} else {
		out.Timeout = 60 * time.Second
	}
	transport := out.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	out.Transport = proxyTransport{fallback: transport}
	out.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &out
}

func Endpoint(target gateway.Target, fallback, path string) (string, error) {
	base := strings.TrimRight(target.BaseURL, "/")
	if base == "" {
		base = fallback
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("invalid provider endpoint")
	}
	return base + "/" + strings.TrimLeft(path, "/"), nil
}

func Request(ctx context.Context, method, endpoint, secret string, body []byte) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid provider request")
	}
	r.Header.Set("Content-Type", "application/json")
	if secret != "" {
		r.Header.Set("Authorization", "Bearer "+secret)
	}
	return r, nil
}

func JSON(client *http.Client, req *http.Request) (out []byte, resultErr error) {
	resp, err := Do(client, req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil && resultErr == nil {
			resultErr = errors.New("task provider response close failed")
		}
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (128<<20)+1))
	if err != nil {
		return nil, errors.New("task provider response read failed")
	}
	if len(body) > 128<<20 {
		return nil, errors.New("task provider response exceeds limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &Rejected{Status: resp.StatusCode}
	}
	if !json.Valid(body) {
		return nil, errors.New("invalid task provider JSON")
	}
	return body, nil
}

func Status(s string) string {
	switch strings.ToLower(s) {
	case "success", "succeeded", "completed", "complete", "done":
		return "completed"
	case "failed", "failure", "error", "canceled", "cancelled":
		return "failed"
	case "running", "processing", "in_progress":
		return "in_progress"
	default:
		return "queued"
	}
}
