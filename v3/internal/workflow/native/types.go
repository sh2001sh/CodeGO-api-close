// Package native defines asynchronous provider contracts without owning billing.
package native

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type Submit struct {
	Action      string
	Model       string
	Body        []byte
	ContentType string
	OriginID    string // upstream task ID for remix
}

type Task struct {
	UpstreamID string
	Action     string
	Model      string
	Data       json.RawMessage
}

type Result struct {
	ID     string
	Status string // queued | in_progress | completed | failed
	Data   json.RawMessage
	URL    string
	Error  string
	Usage  gateway.Usage
	Units  float64 // actual seconds/images, for parent pricing
}

type Adapter interface {
	Submit(context.Context, gateway.Target, Submit) (Result, error)
	Poll(context.Context, gateway.Target, Task) (Result, error)
	Content(context.Context, gateway.Target, Task) (*http.Response, error)
}
