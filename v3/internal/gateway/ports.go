package gateway

import (
	"context"
	"net/http"
)

// Authorizer resolves an API key to a principal. Implementations serve from
// an in-process snapshot; a miss must not hit PostgreSQL on the hot path.
type Authorizer interface {
	Authorize(ctx context.Context, apiKey string) (Principal, error)
}

// Planner returns the ordered candidates for a request and receives attempt
// feedback for cooldowns. First attempt and retries share one decision.
type Planner interface {
	Plan(ctx context.Context, req *Request) ([]Target, error)
	Report(target Target, result AttemptResult)
}

// Settler reserves credits before the upstream call and settles or releases
// them from the Outcome. The Redis Lua implementation lands in M2.
type Settler interface {
	Reserve(ctx context.Context, req *Request) error
	Finalize(ctx context.Context, req *Request, out Outcome) error
}

// Provider adapts one upstream API. It never writes to the client and never
// computes money; it only builds requests and decodes responses into events.
type Provider interface {
	BuildRequest(ctx context.Context, req *Request, target Target) (*http.Request, error)
	Decode(req *Request, resp *http.Response) EventStream
}

// RequestFinalizer signs the final native bytes after channel overrides.
type RequestFinalizer interface {
	FinalizeRequest(context.Context, *http.Request, *Request, Target) error
}

// RawRequestBuilder lets wrappers resolve local references before raw forwarding.
type RawRequestBuilder interface {
	BuildRawRequest(context.Context, *Request, Target) (*http.Request, error)
}

// TransportProvider lets protocol wrappers preserve a native transport, such as
// Spark's WebSocket exchange, while keeping the configured HTTP proxy fallback.
type TransportProvider interface {
	UpstreamTransport(req *Request, fallback http.RoundTripper) http.RoundTripper
}

// ClientProvider resolves a credential's stable HTTP identity and proxy pool.
// Native multi-call providers receive this transport as their fallback.
type ClientProvider func(context.Context, Target) (*http.Client, error)

// TargetPolicy checks channel-specific prompt rules against frozen client input.
type TargetPolicy func(*Request, Target) error

// RequestGuard applies account-level admission once, before planning or billing.
type RequestGuard func(context.Context, *Request) error

// EventKind classifies a decoded upstream event.
type EventKind uint8

const (
	EventData  EventKind = iota + 1 // forward to the client as-is
	EventUsage                      // usage-only; forwarded if the client protocol carries it
	EventDone                       // clean end of stream ([DONE] or equivalent)
	EventError                      // upstream reported an error in-band
)

// Event is one decoded upstream event. Payload is valid until the next Next.
type Event struct {
	Kind      EventKind
	Name      string // native SSE event name; empty for data-only protocols
	Payload   []byte
	Usage     *Usage
	Err       *UpstreamError
	TextBytes int // generated text in this event, for the local usage estimate
}

// EventStream is a pull iterator over upstream events. Next returns io.EOF
// after a clean end; any other error means the stream was cut.
type EventStream interface {
	Next() (Event, error)
	Close() error
}
