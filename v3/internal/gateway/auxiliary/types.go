// Package auxiliary serves billed non-chat gateway APIs.
package auxiliary

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

type Operation string

const (
	Images           Operation = "images/generations"
	ImageEdits       Operation = "images/edits"
	Edits            Operation = "edits"
	Embeddings       Operation = "embeddings"
	Transcriptions   Operation = "audio/transcriptions"
	Translations     Operation = "audio/translations"
	Speech           Operation = "audio/speech"
	Rerank           Operation = "rerank"
	Moderations      Operation = "moderations"
	Completions      Operation = "completions"
	Compact          Operation = "responses/compact"
	Search           Operation = "alpha/search"
	GeminiEmbed      Operation = "gemini/embedContent"
	GeminiBatchEmbed Operation = "gemini/batchEmbedContents"
	GeminiImages     Operation = "gemini/predict"
)

// Input keeps original multipart/binary bytes separate from the JSON billing body.
type Input struct {
	Operation   Operation
	Path        string
	ContentType string
	Body        []byte
}

// Response contains a converted non-stream response and its accounting.
type Response struct {
	Body   []byte
	Header http.Header
	Usage  *gateway.Usage
}

// Adapter implements an actual upstream API; unsupported operations must fail.
type Adapter interface {
	Build(context.Context, *gateway.Request, gateway.Target, Input) (*http.Request, error)
	Decode(context.Context, *gateway.Request, gateway.Target, Input, *http.Response) (Response, error)
}

type Config struct {
	Authorizer       gateway.Authorizer
	Planner          gateway.Planner
	Settler          gateway.Settler
	Limits           gateway.LeaseController
	AuthFailures     gateway.AuthFailureController
	ValidateRequest  func(gateway.Principal, string, *http.Request) error
	RequestGuard     gateway.RequestGuard
	TargetPolicy     gateway.TargetPolicy
	TrustedProxies   []netip.Prefix
	Transports       *httpx.Pool
	Clients          gateway.ClientProvider
	Adapters         map[string]Adapter
	MaxBodyBytes     int64
	MaxResponseBytes int64
	RelayTimeout     time.Duration
	DrainTimeout     time.Duration
	FinalizeTimeout  time.Duration
	Logger           *slog.Logger
}
