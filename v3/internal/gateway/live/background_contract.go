package live

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// BackgroundBilling retains the original hold across processes. Data must
// contain no credentials; Finalize must be idempotent by Request.ID.
type BackgroundBilling interface {
	Reserve(context.Context, *gateway.Request) (json.RawMessage, error)
	Refresh(context.Context, *gateway.Request, json.RawMessage) error
	Finalize(context.Context, *gateway.Request, json.RawMessage, gateway.Outcome) error
}

type PrincipalResolver func(context.Context, int64, int64) (gateway.Principal, error)

type BackgroundJob struct {
	ID                   string            `json:"id"`
	UserID               int64             `json:"user_id"`
	KeyID                int64             `json:"key_id"`
	Group                string            `json:"group"`
	Model                string            `json:"model"`
	Path                 string            `json:"path,omitempty"`
	ChannelID            int64             `json:"channel_id"`
	CredentialID         int64             `json:"credential_id"`
	Body                 []byte            `json:"body"`
	ClientIP             string            `json:"client_ip"`
	PricingHeaders       map[string]string `json:"pricing_headers,omitempty"`
	Reservation          json.RawMessage   `json:"reservation"`
	Status               string            `json:"status"`
	Native               bool              `json:"native"`
	Stream               bool              `json:"stream"`
	CancelRequested      bool              `json:"cancel_requested"`
	Billed               bool              `json:"billed"`
	UpstreamID           string            `json:"upstream_id,omitempty"`
	LastUpstreamSequence int64             `json:"last_upstream_sequence"`
	Snapshot             json.RawMessage   `json:"snapshot,omitempty"`
	Usage                gateway.Usage     `json:"usage"`
	UsageReported        bool              `json:"usage_reported"`
	Delivered            bool              `json:"delivered"`
	GeneratedBytes       int64             `json:"generated_bytes"`
	Error                string            `json:"error,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
	LeaseID              string            `json:"lease_id,omitempty"`
	LeaseUntil           time.Time         `json:"lease_until,omitempty"`
}

type BackgroundEvent struct {
	Sequence int64  `json:"sequence"`
	Type     string `json:"type"`
	Payload  []byte `json:"payload"`
}

type BackgroundJobRepository interface {
	Create(context.Context, BackgroundJob) error
	GetOwned(context.Context, string, int64, int64) (BackgroundJob, error)
	Pending(context.Context, int) ([]string, error)
	Claim(context.Context, string, string, time.Duration) (BackgroundJob, error)
	Save(context.Context, BackgroundJob) error
	Append(context.Context, string, string, BackgroundEvent) (int64, error)
	Events(context.Context, string, int64, int) ([]BackgroundEvent, error)
	Cancel(context.Context, string, int64, int64) (BackgroundJob, error)
}
