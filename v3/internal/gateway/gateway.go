package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/httpx"
	"github.com/sh2001sh/new-api/v3/pkg/metrics"
)

// Config holds pipeline limits. Zero values select the defaults below.
type Config struct {
	MaxBodyBytes    int64
	HeaderTimeout   time.Duration // streaming: request sent -> upstream headers
	RelayTimeout    time.Duration // whole upstream exchange
	DrainTimeout    time.Duration // keep reading after the client leaves, for usage
	Heartbeat       time.Duration // SSE keep-alive when idle
	FinalizeTimeout time.Duration
	TrustedProxies  []netip.Prefix             // only these peers may supply forwarded client addresses
	RequestID       func(*http.Request) string // verified internal reconciliation identity
}

func (c Config) withDefaults() Config {
	if c.MaxBodyBytes == 0 {
		c.MaxBodyBytes = 32 << 20
	}
	if c.HeaderTimeout == 0 {
		c.HeaderTimeout = 60 * time.Second
	}
	if c.RelayTimeout == 0 {
		c.RelayTimeout = 30 * time.Minute
	}
	if c.DrainTimeout == 0 {
		c.DrainTimeout = 30 * time.Second
	}
	if c.Heartbeat == 0 {
		c.Heartbeat = 15 * time.Second
	}
	if c.FinalizeTimeout == 0 {
		c.FinalizeTimeout = 5 * time.Second
	}
	return c
}

// Deps are the collaborators the pipeline needs; cmd/gateway wires them.
type Deps struct {
	Config       Config
	Authorizer   Authorizer
	Planner      Planner
	Settler      Settler
	Limits       LeaseController
	AuthFailures AuthFailureController
	Providers    map[string]Provider
	Transports   *httpx.Pool
	Clients      ClientProvider
	Samples      SampleRecorder
	Requests     RequestRecorder
	TargetPolicy TargetPolicy
	RequestGuard RequestGuard
	Metrics      *metrics.Recorder
	Logger       *slog.Logger
}

// Gateway serves the relay endpoints.
type Gateway struct {
	cfg          Config
	auth         Authorizer
	planner      Planner
	settler      Settler
	limits       LeaseController
	authFailures AuthFailureController
	providers    map[string]Provider
	transports   *httpx.Pool
	clients      ClientProvider
	samples      SampleRecorder
	requests     RequestRecorder
	targetPolicy TargetPolicy
	requestGuard RequestGuard
	metrics      *metrics.Recorder
	log          *slog.Logger
}

// New validates dependencies and returns a Gateway.
func New(d Deps) (*Gateway, error) {
	if d.Authorizer == nil || d.Planner == nil || d.Settler == nil || len(d.Providers) == 0 {
		return nil, errors.New("gateway: authorizer, planner, settler and providers are required")
	}
	if d.Transports == nil {
		d.Transports = httpx.NewPool(httpx.TransportConfig{})
	}
	if d.Metrics == nil {
		d.Metrics = metrics.New()
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &Gateway{cfg: d.Config.withDefaults(), auth: d.Authorizer, planner: d.Planner, settler: d.Settler,
		limits: d.Limits, authFailures: d.AuthFailures,
		providers: d.Providers, transports: d.Transports, clients: d.Clients, samples: d.Samples, requests: d.Requests, targetPolicy: d.TargetPolicy, requestGuard: d.RequestGuard, metrics: d.Metrics, log: d.Logger}, nil
}

// Register mounts the relay routes on mux.
func (g *Gateway) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", g.serveChat)
	mux.HandleFunc("POST /v1/responses", g.serveChat)
	mux.HandleFunc("POST /v1/messages", g.serveChat)
	mux.HandleFunc("POST /v1beta/models/{action}", g.serveChat)
	mux.HandleFunc("POST /v1/models/{action}", g.serveChat)
	mux.Handle("GET /metrics", g.metrics.Handler())
}

func (g *Gateway) serveChat(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	id := ""
	if g.cfg.RequestID != nil {
		id = g.cfg.RequestID(r)
	}
	if id == "" {
		id = newRequestID()
	}
	req := &Request{ID: id, Received: now, Timeline: g.metrics.Start(now)}
	w.Header().Set("X-Request-Id", req.ID)

	if e := g.admit(w, r, req); e != nil {
		g.recordRejected(req, e.status, e.code)
		writeProtocolError(w, req.Protocol, e.status, e.typ, e.code, e.message)
		return
	}
	if err := g.settler.Reserve(r.Context(), req); err != nil {
		switch {
		case errors.Is(err, ErrInsufficientCredits):
			g.recordRejected(req, http.StatusPaymentRequired, "insufficient_credits")
			writeProtocolError(w, req.Protocol, http.StatusPaymentRequired, "insufficient_quota", "insufficient_credits", "not enough credits")
			return
		case !errors.Is(err, ErrBillingUnavailable):
			g.log.Error("reserve failed", "request_id", req.ID, "err", err)
		}
		writeProtocolError(w, req.Protocol, http.StatusServiceUnavailable, "api_error", "billing_unavailable", "billing is temporarily unavailable")
		g.recordRejected(req, http.StatusServiceUnavailable, "billing_unavailable")
		return
	}
	req.Timeline.Mark(metrics.StageReserve, time.Now())
	if g.samples != nil && g.samples.ShouldSample(req.ID) {
		req.sample = NewResponseSample(g.samples.MaxBytes())
	}

	cs := newClientStream(w, req.Stream, req.ID, req.Protocol)
	cs.startHeartbeat(g.cfg.Heartbeat)
	out := g.execute(r.Context(), req, cs)
	cs.stopHeartbeat()
	req.Timeline.Mark(metrics.StageStream, time.Now())
	g.finalize(req, out)
}

// admit runs Parse, Authorize and Plan. None of them touch PostgreSQL.
func (g *Gateway) admit(w http.ResponseWriter, r *http.Request, req *Request) *clientError {
	if e := parseRequest(w, r, g.cfg.MaxBodyBytes, req); e != nil {
		return e
	}
	req.Timeline.Mark(metrics.StageParse, time.Now())

	if e := g.authenticate(r, req); e != nil {
		return e
	}
	req.Timeline.Mark(metrics.StageAuthorize, time.Now())
	if g.requestGuard != nil {
		if err := g.requestGuard(r.Context(), req); err != nil {
			var refusal *UpstreamError
			if errors.As(err, &refusal) {
				return &clientError{refusal.Status, refusal.Type, refusal.Code, refusal.Message}
			}
			return &clientError{http.StatusServiceUnavailable, "api_error", "request_guard_unavailable", "request admission is temporarily unavailable"}
		}
	}

	if e := g.plan(r, req); e != nil {
		return e
	}
	req.Timeline.Mark(metrics.StagePlan, time.Now())
	return nil
}

// authenticate resolves the API key to a principal and checks it against
// request policy. It also records auth failures so repeated bad keys from
// the same client address get rate limited by authFailures.
func (g *Gateway) authenticate(r *http.Request, req *Request) *clientError {
	key := apiKeyFrom(r)
	address := g.clientAddress(r)
	if g.authFailures != nil && g.authFailures.Blocked(address) {
		return errAuthLimited
	}
	if key == "" {
		if g.authFailures != nil {
			g.authFailures.Failed(address)
		}
		return errMissingKey
	}
	principal, err := g.auth.Authorize(r.Context(), key)
	switch {
	case errors.Is(err, ErrAuthUnavailable):
		g.log.Error("authorize unavailable", "request_id", req.ID, "err", err)
		return errAuthDown
	case err != nil:
		if g.authFailures != nil {
			g.authFailures.Failed(address)
		}
		return errInvalidKey
	}
	principal, err = requestedPrincipal(principal, r)
	if err != nil {
		var refusal *UpstreamError
		if errors.As(err, &refusal) {
			return &clientError{refusal.Status, refusal.Type, refusal.Code, refusal.Message}
		}
		return &clientError{http.StatusForbidden, "permission_error", "group_not_allowed", "API key does not allow this group"}
	}
	req.Principal = principal
	if policyErr := ValidateRequestPolicy(principal, req.Model, r, g.cfg.TrustedProxies); policyErr != nil {
		var upstreamErr *UpstreamError
		if errors.As(policyErr, &upstreamErr) {
			return &clientError{upstreamErr.Status, upstreamErr.Type, upstreamErr.Code, upstreamErr.Message}
		}
		return &clientError{http.StatusForbidden, "permission_error", "request_not_permitted", "API key does not permit this request"}
	}
	return nil
}

// plan resolves the ordered candidate targets for req and applies the
// target policy to the first one.
func (g *Gateway) plan(r *http.Request, req *Request) *clientError {
	targets, err := g.planner.Plan(r.Context(), req)
	if err != nil || len(targets) == 0 {
		var refusal *UpstreamError
		if errors.As(err, &refusal) {
			return &clientError{refusal.Status, refusal.Type, refusal.Code, refusal.Message}
		}
		return errNoRoute
	}
	req.Targets = targets
	if g.targetPolicy != nil {
		if err := g.targetPolicy(req, targets[0]); err != nil {
			var policyErr *UpstreamError
			if errors.As(err, &policyErr) {
				return &clientError{policyErr.Status, policyErr.Type, policyErr.Code, policyErr.Message}
			}
			return &clientError{http.StatusServiceUnavailable, "api_error", "target_policy_unavailable", "channel policy is unavailable"}
		}
	}
	return nil
}

// execute walks the RoutePlan. Failover happens only while nothing has been
// delivered; after the first data event the attempt's result is final.
func (g *Gateway) execute(ctx context.Context, req *Request, cs *clientStream) Outcome {
	var last finish
	var lastRes AttemptResult
	var lastTarget *Target
	for i := range req.Targets {
		target := req.Targets[i]
		started := time.Now()
		f, res := g.attempt(ctx, req, target, cs)
		req.Attempts = append(req.Attempts, Attempt{Target: target, Result: res, Duration: time.Since(started)})
		g.planner.Report(target, res)
		last, lastRes, lastTarget = f, res, &req.Targets[i]
		if f.err == nil && res.Err != nil && !f.empty {
			last.err = res.Err
		}

		if res.OK || f.delivered || f.clientGone || !res.Retryable {
			break
		}
	}

	out := decide(last)
	out.Target = lastTarget
	switch {
	case !out.Delivered && out.Terminal != TerminalClientCanceled:
		// Every candidate failed before any content: report the last error.
		if e := lastRes.Err; e != nil {
			cs.fail(e)
		}
	case out.Terminal == TerminalCompleted || out.Terminal == TerminalCompletedNoUsage:
		cs.done()
	}
	return out
}

// finalize settles or releases the reservation. It runs detached from the
// client so a disconnect cannot skip billing, and failures are logged loudly.
func (g *Gateway) finalize(req *Request, out Outcome) {
	ctx, cancel := context.WithTimeout(context.Background(), g.cfg.FinalizeTimeout)
	defer cancel()
	err := g.settler.Finalize(ctx, req, out)
	if err != nil {
		g.log.Error("finalize failed", "request_id", req.ID, "terminal", out.Terminal.String(), "err", err)
	}
	RecordRequest(g.requests, req, out, err == nil)
	req.Timeline.Mark(metrics.StageFinalize, time.Now())
	if req.sample != nil {
		g.samples.Record(req, out, req.sample.JSON())
	}
}
