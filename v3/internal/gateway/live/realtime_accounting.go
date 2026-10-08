package live

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type relayEnd struct {
	client bool
	err    error
}

// The mutex serializes client admission with terminal accounting. No client
// frame is forwarded between releasing a completed hold and reserving the next.
type realtimeAccounting struct {
	mu            sync.Mutex
	req           *gateway.Request
	out           gateway.Outcome
	seen          map[string]bool
	pending       bool
	usageReported bool
	outputChars   int64
}

func addRealtimeUsage(total *gateway.Usage, usage gjson.Result) {
	total.PromptTokens += usage.Get("input_tokens").Int()
	total.CompletionTokens += usage.Get("output_tokens").Int()
	total.CachedTokens += usage.Get("input_token_details.cached_tokens").Int()
	total.AudioInputTokens += usage.Get("input_token_details.audio_tokens").Int()
	total.AudioOutputTokens += usage.Get("output_token_details.audio_tokens").Int()
	// Providers have shipped both singular and plural detail field names.
	if !usage.Get("input_token_details").Exists() {
		total.CachedTokens += usage.Get("input_tokens_details.cached_tokens").Int()
		total.AudioInputTokens += usage.Get("input_tokens_details.audio_tokens").Int()
	}
	if !usage.Get("output_token_details").Exists() {
		total.AudioOutputTokens += usage.Get("output_tokens_details.audio_tokens").Int()
	}
}

func (h *Handler) completeRealtimeTurn(ctx context.Context, r *http.Request, target gateway.Target, a *realtimeAccounting) error {
	if a.req == nil {
		return gateway.ErrBillingUnavailable
	}
	observation := gateway.Observation{Delivered: a.out.Delivered,
		Estimate: gateway.Usage{PromptTokens: (int64(len(a.req.Body)) + 3) / 4, CompletionTokens: (a.outputChars + 3) / 4, Estimated: true}}
	if a.usageReported {
		observation.Usage = &a.out.Usage
	}
	completed := gateway.Decide(observation)
	completed.Target = a.out.Target
	previous := a.req
	// Even a failed durable finalization cannot be charged twice by the
	// disconnect path. The settler's WAL/recovery owns admitted work.
	a.req = nil
	a.out = completed
	if err := h.finalizeResult(previous, completed); err != nil {
		return err
	}
	principal, err := h.cfg.Auth.Authorize(ctx, websocketAPIKey(r))
	if err != nil {
		return err
	}
	if principal.UserID != previous.Principal.UserID || principal.KeyID != previous.Principal.KeyID {
		return gateway.ErrInvalidKey
	}
	if err := h.validatePolicy(principal, previous.Model, r); err != nil {
		return &gateway.UpstreamError{Status: 403, Code: "request_not_permitted", Message: "realtime request is no longer permitted"}
	}
	next := &gateway.Request{ID: requestID(), Received: time.Now(), Protocol: previous.Protocol,
		Path: previous.Path, Body: previous.Body, Model: previous.Model, Stream: true,
		Principal: principal, PricingHeaders: previous.PricingHeaders}
	if failure := h.requestGuardFailure(ctx, next); failure != nil {
		return failure
	}
	targets, err := h.Plan(ctx, next)
	if err != nil {
		return err
	}
	var current *gateway.Target
	for _, candidate := range targets {
		if candidate.ChannelID == target.ChannelID && candidate.CredentialID == target.CredentialID {
			value := candidate
			current = &value
			break
		}
	}
	if current == nil {
		return errors.New("live: realtime route is no longer available")
	}
	next.Targets = []gateway.Target{*current}
	if failure := h.targetPolicyFailure(next, *current); failure != nil {
		return failure
	}
	if err := h.cfg.Settler.Reserve(ctx, next); err != nil {
		return err
	}
	next.Attempts = []gateway.Attempt{{Target: *current, Result: gateway.AttemptResult{OK: true, Status: 101}}}
	a.req, a.out = next, gateway.Outcome{Target: current}
	a.pending, a.usageReported, a.outputChars = false, false, 0
	return nil
}
