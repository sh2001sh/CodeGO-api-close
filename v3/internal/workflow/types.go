// Package workflow owns authenticated asynchronous generation tasks.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

var (
	ErrNotFound = errors.New("task not found")
	ErrConflict = errors.New("task lease conflict")
)

// Reservation must be durable and contain no credentials. Finalize must be
// idempotent on Request.ID across process restarts and repeated reconciliation.
type Reservation struct {
	Data             json.RawMessage `json:"data"`
	EstimatedCredits credits.Micro   `json:"estimated_credits"`
}

type Settler interface {
	Reserve(context.Context, *gateway.Request) (Reservation, error)
	Finalize(context.Context, *gateway.Request, Reservation, native.Result) (credits.Micro, error)
}

// ResolveTarget loads the current credential by persisted IDs, not by a new
// routing decision. Never persist the returned Target.Secret in task data.
type TargetResolver func(context.Context, int64, int64) (gateway.Target, error)

type Task struct {
	ID             string            `json:"id"`
	UserID         int64             `json:"-"`
	KeyID          int64             `json:"-"`
	Group          string            `json:"-"`
	TargetGroup    string            `json:"-"`
	Provider       string            `json:"-"`
	ChannelID      int64             `json:"-"`
	CredentialID   int64             `json:"-"`
	Model          string            `json:"model"`
	UpstreamModel  string            `json:"-"`
	UpstreamID     string            `json:"-"`
	Action         string            `json:"-"`
	Status         string            `json:"status"`
	Body           []byte            `json:"-"`
	PricingHeaders map[string]string `json:"-"`
	Reservation    Reservation       `json:"-"`
	Data           json.RawMessage   `json:"-"`
	URL            string            `json:"-"`
	Error          string            `json:"-"`
	Usage          gateway.Usage     `json:"-"`
	Units          float64           `json:"-"`
	CostState      string            `json:"-"`
	ActualCredits  credits.Micro     `json:"-"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"-"`
	LeaseID        string            `json:"-"`
	Historical     bool              `json:"-"`
}

type TaskRepository interface {
	Create(context.Context, Task) error
	GetOwned(context.Context, string, int64) (Task, error)
	Pending(context.Context, int) ([]Task, error)
	Claim(context.Context, string, string, time.Time) (Task, error)
	Save(context.Context, Task) error // requires matching LeaseID if task is leased
}

func (t Task) Request() *gateway.Request {
	return &gateway.Request{ID: t.ID, Received: t.CreatedAt, Body: t.Body, Model: t.Model, PricingHeaders: t.PricingHeaders,
		Principal: gateway.Principal{UserID: t.UserID, KeyID: t.KeyID, Group: t.Group},
		Targets:   []gateway.Target{{ChannelID: t.ChannelID, CredentialID: t.CredentialID, Provider: t.Provider, UpstreamModel: t.UpstreamModel, Group: t.TargetGroup}}}
}

func (t Task) Native() native.Task {
	return native.Task{UpstreamID: t.UpstreamID, Action: t.Action, Model: t.UpstreamModel, Data: t.Data}
}
