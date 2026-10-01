package live

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/azure"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/codex"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

var backgroundTransports = httpx.NewPool(httpx.TransportConfig{})

func (h *Handler) registerBackground(mux *http.ServeMux) {
	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		mux.HandleFunc("GET "+path+"/{id}", h.serveBackground)
		mux.HandleFunc("POST "+path+"/{id}/cancel", h.serveBackground)
	}
}

// Retrieval and cancellation address an existing generation. They never reserve
// or finalize credits again; the creating request owns that lifecycle.
func (h *Handler) serveBackground(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorize(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if principal.KeyID <= 0 || !backgroundID(id) {
		writeError(w, http.StatusNotFound, "response_not_found", "response was not found")
		return
	}
	if strings.HasPrefix(id, "resp_bg_") {
		h.serveBackgroundJob(w, r, principal, id)
		return
	}
	locator, ok := h.loadBackgroundLocator(w, r, principal, id)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.cfg.SessionTimeout)
	defer cancel()
	req, upstream, target, ok := h.buildBackgroundUpstreamRequest(ctx, w, r, principal, locator, id)
	if !ok {
		return
	}
	release, ok := h.acquireBackgroundLimit(w, ctx, req, target)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	resp, ok := h.doBackgroundUpstreamRequest(w, r, ctx, target, upstream)
	if !ok {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	h.writeBackgroundResponse(w, resp, target, req.ID)
}

// acquireBackgroundLimit acquires the rate-limit lease for a retrieval/cancel request, if limits
// are configured. The returned release func (nil when limits are disabled) must be deferred by
// the caller to run after the response body has been fully written, not before.
func (h *Handler) acquireBackgroundLimit(w http.ResponseWriter, ctx context.Context, req *gateway.Request, target gateway.Target) (release func(), ok bool) {
	if h.cfg.Limits == nil {
		return nil, true
	}
	if err := h.cfg.Limits.Acquire(ctx, req, target); err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, gateway.ErrRateLimited) || errors.Is(err, gateway.ErrTargetBusy) {
			status = http.StatusTooManyRequests
		}
		writeError(w, status, "response_limit_reached", "the response route cannot admit this request")
		return nil, false
	}
	return func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), h.cfg.FinalizeTimeout)
		defer releaseCancel()
		if err := h.cfg.Limits.Release(releaseCtx, req, target); err != nil {
			h.cfg.Logger.Error("background lease release failed", "request_id", req.ID, "err", err)
		}
	}, true
}

// doBackgroundUpstreamRequest acquires an upstream client and sends the prepared retrieval/cancel
// request, mapping transport failures to the appropriate HTTP error response.
func (h *Handler) doBackgroundUpstreamRequest(w http.ResponseWriter, r *http.Request, ctx context.Context, target gateway.Target, upstream *http.Request) (*http.Response, bool) {
	client, err := h.upstreamClient(ctx, target)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "response_proxy_unavailable", "the response client is unavailable")
		return nil, false
	}
	resp, err := client.Do(upstream)
	if err != nil {
		if r.Context().Err() != nil {
			return nil, false
		}
		status := http.StatusBadGateway
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, "response_upstream_unavailable", "the upstream response cannot be reached")
		return nil, false
	}
	return resp, true
}

// loadBackgroundLocator looks up and authorizes the stored response locator for id, writing
// the appropriate HTTP error and returning ok=false if it cannot be used.
func (h *Handler) loadBackgroundLocator(w http.ResponseWriter, r *http.Request, principal gateway.Principal, id string) (locator Locator, ok bool) {
	locator, err := h.cfg.Repository.Get(r.Context(), id, principal.UserID, principal.KeyID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "response_not_found", "response was not found")
		return locator, false
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "response_store_unavailable", "response lookup is unavailable")
		return locator, false
	}
	if locator.ID != id || locator.UserID != principal.UserID || locator.KeyID != principal.KeyID {
		writeError(w, http.StatusNotFound, "response_not_found", "response was not found")
		return locator, false
	}
	if err := h.validatePolicy(principal, "", r); err != nil {
		writeError(w, http.StatusForbidden, "request_not_permitted", err.Error())
		return locator, false
	}
	return locator, true
}

// buildBackgroundUpstreamRequest parses the retrieval/cancel query, resolves the locator's
// route and builds the upstream HTTP request to send, writing the appropriate HTTP error and
// returning ok=false if any step fails.
func (h *Handler) buildBackgroundUpstreamRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, principal gateway.Principal, locator Locator, id string) (req *gateway.Request, upstream *http.Request, target gateway.Target, ok bool) {
	query, err := backgroundQuery(r.URL.RawQuery, r.Method == http.MethodGet)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return nil, nil, target, false
	}
	target, err = h.cfg.Resolve(ctx, locator.ChannelID, locator.CredentialID)
	if err != nil || locator.ChannelID <= 0 || locator.CredentialID <= 0 || target.ChannelID != locator.ChannelID || target.CredentialID != locator.CredentialID {
		writeError(w, http.StatusServiceUnavailable, "response_route_unavailable", "the response route is unavailable")
		return nil, nil, target, false
	}
	req = &gateway.Request{ID: requestID(), Received: time.Now(), Protocol: gateway.ProtocolResponses, Model: locator.Model,
		Body: []byte(`{}`), Principal: principal, Stream: query.Get("stream") == "true"}
	upstream, err = backgroundRequest(ctx, req, target, id, r.Method, query)
	if err != nil {
		writeError(w, http.StatusBadGateway, "response_route_invalid", "the response route cannot be used")
		return nil, nil, target, false
	}
	return req, upstream, target, true
}

// writeBackgroundResponse maps the upstream status and relays its body (streaming or buffered)
// to the client, writing an HTTP error instead if the upstream response is unusable.
func (h *Handler) writeBackgroundResponse(w http.ResponseWriter, resp *http.Response, target gateway.Target, requestID string) {
	resp.StatusCode = gateway.MapUpstreamStatus(resp.StatusCode, target)
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		writeError(w, http.StatusBadGateway, "response_upstream_redirect", "the response route returned a redirect")
		return
	}
	for _, name := range []string{"Content-Type", "Cache-Control", "Etag", "Retry-After", "X-Request-Id", "Openai-Processing-Ms", "X-Codex-Turn-State"} {
		if value := resp.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if _, ok := w.(http.Flusher); !ok {
			writeError(w, http.StatusInternalServerError, "response_stream_unavailable", "the response cannot be streamed")
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(resp.StatusCode)
		h.copyBackgroundStream(w, resp.Body, requestID)
		return
	}
	// Buffer snapshots/errors before writing status, so a truncated or oversized
	// body is an explicit gateway error instead of a successful partial result.
	limit := h.cfg.MaxBodyBytes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limit = min(limit, 64<<10)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		writeError(w, http.StatusBadGateway, "response_body_invalid", "the upstream response is truncated or too large")
		return
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(body); err != nil {
		h.cfg.Logger.Debug("background response client disconnected", "request_id", requestID)
	}
}

func backgroundID(id string) bool {
	if len(id) == 0 || len(id) > 256 {
		return false
	}
	for _, c := range id {
		if c == '-' || c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

func backgroundQuery(raw string, retrieval bool) (url.Values, error) {
	input, err := url.ParseQuery(raw)
	if err != nil {
		return nil, errors.New("invalid query string")
	}
	query := make(url.Values)
	if !retrieval {
		return query, nil
	}
	if values, ok := input["stream"]; ok {
		if len(values) != 1 {
			return nil, errors.New("stream must be a single boolean")
		}
		stream, err := strconv.ParseBool(strings.TrimSpace(values[0]))
		if err != nil {
			return nil, errors.New("stream must be a boolean")
		}
		query.Set("stream", strconv.FormatBool(stream))
	}
	if values, ok := input["starting_after"]; ok {
		if len(values) != 1 {
			return nil, errors.New("starting_after must be a single integer sequence number")
		}
		cursor, err := strconv.ParseInt(strings.TrimSpace(values[0]), 10, 64)
		if err != nil || cursor < -1 {
			return nil, errors.New("starting_after must be an integer sequence number greater than or equal to -1")
		}
		query.Set("starting_after", strconv.FormatInt(cursor, 10))
	}
	return query, nil
}

// backgroundRequestBase builds the per-provider base request used for both retrieval and
// cancellation of a background job: either a direct GET/POST against the OpenAI-compatible
// Responses endpoint, or a provider-built request for providers whose generation request
// shape can be reused as a base (Azure, Codex).
func backgroundRequestBase(ctx context.Context, req *gateway.Request, target gateway.Target, method string) (*http.Request, error) {
	switch target.Provider {
	case "openai", "responses", "openaimax", "openai_max":
		endpoint, parseErr := url.Parse(target.BaseURL)
		if parseErr != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
			return nil, errors.New("invalid response endpoint")
		}
		endpoint.Path = strings.TrimRight(endpoint.Path, "/")
		if !strings.HasSuffix(endpoint.Path, "/responses") {
			if !strings.HasSuffix(endpoint.Path, "/v1") {
				endpoint.Path += "/v1"
			}
			endpoint.Path += "/responses"
		}
		endpoint.RawPath = ""
		out, err := http.NewRequestWithContext(ctx, method, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		out.Header.Set("Authorization", "Bearer "+target.Secret)
		out.Header.Set("Content-Type", "application/json")
		if req.Stream {
			out.Header.Set("Accept", "text/event-stream")
		}
		return out, nil
	case azure.ID:
		return gateway.BuildProviderRequest(ctx, azure.Provider{}, req, target)
	case codex.ID:
		return gateway.BuildProviderRequest(ctx, codex.Provider{}, req, target)
	default:
		return nil, errors.New("provider does not expose native response retrieval")
	}
}

func backgroundRequest(ctx context.Context, req *gateway.Request, target gateway.Target, id, method string, query url.Values) (*http.Request, error) {
	out, err := backgroundRequestBase(ctx, req, target, method)
	if err != nil {
		return nil, err
	}
	if out.Body != nil {
		_ = out.Body.Close()
	}
	if out.URL.Host == "" || out.URL.User != nil || out.URL.Fragment != "" || (out.URL.Scheme != "https" && out.URL.Scheme != "http") {
		return nil, errors.New("invalid response endpoint")
	}
	out.Method, out.Body, out.GetBody, out.ContentLength = method, nil, nil, 0
	out.URL.Path = strings.TrimRight(out.URL.Path, "/") + "/" + id
	out.URL.RawPath = ""
	if method == http.MethodPost {
		out.URL.Path += "/cancel"
	}
	upstreamQuery := out.URL.Query()
	for name, values := range query {
		upstreamQuery[name] = values
	}
	out.URL.RawQuery = upstreamQuery.Encode()
	// Metadata calls preserve channel headers and status mapping without
	// applying generation-only JSON policy to a bodyless GET or cancel.
	target.ParamOverride = nil
	if err := gateway.ApplyUpstreamRequest(out, req, target); err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handler) copyBackgroundStream(w http.ResponseWriter, body io.Reader, id string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.cfg.Logger.Error("background stream cannot flush", "request_id", id)
		return
	}
	flusher.Flush()
	buffer := make([]byte, 32<<10)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return
			}
			flusher.Flush()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				h.cfg.Logger.Warn("background response stream interrupted", "request_id", id)
				_, _ = io.WriteString(w, "\n\nevent: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"upstream_error\",\"code\":\"response_stream_interrupted\",\"message\":\"upstream response stream was interrupted\"}}\n\n")
				flusher.Flush()
			}
			return
		}
	}
}
