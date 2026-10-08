package routing

import (
	"errors"
	"net/http"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestUnknownModelIsClientErrorWhileConfiguredUnavailableModelRemainsTransient(t *testing.T) {
	e := newEnv(snapshotOf("weighted", chanSpec{id: 1, weight: 1, creds: 0}), Config{})
	known := request("")
	_, err := e.planner.Plan(ctx, known)
	var refusal *gateway.UpstreamError
	if !errors.Is(err, ErrNoRoute) || errors.As(err, &refusal) {
		t.Fatalf("configured model with no credentials must remain transient: %v", err)
	}
	unknown := request("")
	unknown.Model = "not-a-configured-model"
	_, err = e.planner.Plan(ctx, unknown)
	if !errors.Is(err, ErrNoRoute) || !errors.As(err, &refusal) || refusal.Status != http.StatusNotFound || refusal.Code != "model_not_found" {
		t.Fatalf("unknown model must be a non-retryable client error: %v", err)
	}
	_, err = newEnv(nil, Config{}).planner.Plan(ctx, unknown)
	refusal = nil
	if !errors.Is(err, ErrNoRoute) || errors.As(err, &refusal) {
		t.Fatalf("missing snapshot must remain transient: %v", err)
	}
}
