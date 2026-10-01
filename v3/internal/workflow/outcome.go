package workflow

import (
	"reflect"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

// OutcomeFor uses the shared gateway terminal decision for an async terminal.
// A queued task is not a terminal and must never be passed here for settlement.
func OutcomeFor(result native.Result, target *gateway.Target) gateway.Outcome {
	observation := gateway.Observation{Delivered: result.Status == "completed", Usage: &result.Usage}
	if result.Status != "completed" {
		observation.Err = &gateway.UpstreamError{Code: "task_failed", Message: result.Error}
	}
	if result.Status == "completed" && result.Units == 0 && reflect.ValueOf(result.Usage).IsZero() {
		observation.Usage = nil
		observation.Estimate = gateway.Usage{Estimated: true}
	}
	out := gateway.Decide(observation)
	out.Target = target
	return out
}
