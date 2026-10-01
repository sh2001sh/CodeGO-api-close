package billing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// WorkflowSettler preserves the gateway's frozen pricing and funding choices
// across process restarts. It refuses local-only outage admission: an async
// task can outlive the process that owns that allowance.
type WorkflowSettler struct {
	settler *Settler
	kind    string
}

func NewWorkflowSettler(settler *Settler) *WorkflowSettler {
	return &WorkflowSettler{settler: settler}
}

var _ workflow.Settler = (*WorkflowSettler)(nil)

func (w *WorkflowSettler) taskSettler(admission bool) *Settler {
	s := w.settler
	cfg := s.cfg
	cfg.DoneTTL = 0 // task completion markers survive arbitrarily delayed reconciliation
	result := &Settler{cfg: cfg, rdb: s.rdb, snapshot: s.snapshot, accounts: s.accounts, loader: s.loader,
		log: s.log, br: s.br, local: s.local, wal: s.wal}
	if admission {
		result.wal = nil
	}
	return result
}

// Persist is atomic across every funding and API-key budget hold. Submission
// may only follow a successful return, so an interrupted promotion cannot have
// created an upstream task whose reservation still has a TTL.
var workflowPersistScript = redis.NewScript(`
for _,key in ipairs(KEYS) do
 if redis.call('EXISTS',key)==0 then return redis.error_reply('task hold missing') end
end
for _,key in ipairs(KEYS) do
 redis.call('HSET',key,'async_request',ARGV[1],'async_user',ARGV[2],'async_key',ARGV[3],'async_kind',ARGV[4])
 redis.call('PERSIST',key)
end
return 1`)

func (w *WorkflowSettler) Reserve(ctx context.Context, req *gateway.Request) (workflow.Reservation, error) {
	if req == nil || req.ID == "" || req.Principal.UserID <= 0 || req.Principal.KeyID <= 0 || len(req.Targets) != 1 {
		return workflow.Reservation{}, errors.New("billing: invalid task request")
	}
	copyReq := *req
	copyReq.PricingHeaders = safePricingHeaders(req.PricingHeaders)
	s := w.taskSettler(true)
	if err := s.Reserve(ctx, &copyReq); err != nil {
		return workflow.Reservation{}, err
	}
	h := copyReq.Reserve.(*hold)
	reservation, err := encodeWorkflowHold(&copyReq, h)
	if err != nil {
		return workflow.Reservation{}, err
	}
	if err := w.persistHold(ctx, &copyReq, h); err != nil {
		return workflow.Reservation{}, err
	}
	req.Reserve = h
	return reservation, nil
}

func (w *WorkflowSettler) persistHold(ctx context.Context, req *gateway.Request, h *hold) error {
	reservationKeys := []string{h.keys.reservation}
	if len(h.funding) > 0 {
		reservationKeys = nil
		for _, p := range h.funding {
			reservationKeys = append(reservationKeys, p.keys.reservation)
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, w.settler.cfg.RedisTimeout)
	defer cancel()
	if err := workflowPersistScript.Run(callCtx, w.settler.rdb, reservationKeys, req.ID, req.Principal.UserID, req.Principal.KeyID, w.kind).Err(); err != nil {
		// No upstream submission is authorized after this error. A partially
		// admitted normal hold is reclaimed by the ordinary sweeper.
		return fmt.Errorf("%w: persist task hold: %v", gateway.ErrBillingUnavailable, err)
	}
	return nil
}

func (w *WorkflowSettler) Finalize(ctx context.Context, req *gateway.Request, reservation workflow.Reservation, result native.Result) (credits.Micro, error) {
	if result.Status != "completed" && result.Status != "failed" {
		return 0, errors.New("billing: task is not terminal")
	}
	if result.Units < 0 || math.IsNaN(result.Units) || math.IsInf(result.Units, 0) {
		return 0, errors.New("billing: invalid task units")
	}
	h, err := restoreWorkflowHold(req, reservation)
	if err != nil {
		return 0, err
	}
	if actual, found, err := w.committedActual(ctx, h, result.Status); found {
		return actual, err
	}
	copyReq := *req
	copyReq.Reserve = h
	target, err := frozenWorkflowTarget(h, copyReq.Targets[0])
	if err != nil {
		return 0, err
	}
	out := gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput, Target: &target, Usage: result.Usage}
	var actual credits.Micro
	if result.Status == "completed" {
		out, actual, err = w.completedWorkflowOutcome(h, out, result)
		if err != nil {
			return 0, err
		}
	}
	if err := w.taskSettler(false).Finalize(ctx, &copyReq, out); err != nil {
		return 0, err
	}
	if committed, found, err := w.committedActual(ctx, h, result.Status); found || err != nil {
		return committed, err
	}
	// WAL append is durable, but cap-sensitive actual money is only known
	// after its Lua replay. Keep the workflow unbilled until Redis confirms it.
	return actual, fmt.Errorf("%w: task settlement pending WAL replay", gateway.ErrBillingUnavailable)
}

// completedWorkflowOutcome fills in a completed task's usage (deriving it
// from the frozen price's billing unit, or falling back to the admission
// estimate when the provider reported none) and computes its settlement
// price.
func (w *WorkflowSettler) completedWorkflowOutcome(h *hold, out gateway.Outcome, result native.Result) (gateway.Outcome, credits.Micro, error) {
	out.Terminal, out.Charge, out.Delivered = gateway.TerminalCompleted, true, true
	var err error
	unit, _ := h.price.Rules["billing_unit"].(string)
	if unit != "" {
		// Veo's source API commonly returns only a generated video URI.
		// V2 retains the admitted duration in that case; label the frozen
		// estimate instead of inventing measured units or leaving money held.
		if unit == "video_second" && result.Units == 0 && out.Usage.VideoDurationMicros == 0 && h.price.Rules["veo_duration_micros"] != nil {
			estimate, err := pricing.EstimateUsageForPrice(h.pricingInput.Body, h.price, w.settler.cfg.Estimate)
			if err != nil {
				return out, 0, err
			}
			out.Usage.VideoDurationMicros, out.Usage.Estimated = estimate.VideoDurationMicros, true
		}
		units := strconv.FormatFloat(result.Units, 'f', -1, 64)
		if unit == "audio_character" {
			units = "0" // native Units means seconds/images, never TTS characters
		}
		out.Usage, err = pricing.WithMediaUnits(out.Usage, unit, units)
		if err != nil {
			return out, 0, err
		}
	}
	// v2 per-call tasks retain their published price even without usage.
	// Token-priced providers that omit accounting retain the frozen
	// admission estimate rather than silently producing a free task.
	if unit == "" && h.price.Mode != "per_request" && emptyWorkflowUsage(out.Usage) {
		out.Usage, err = pricing.EstimateUsageForPrice(h.pricingInput.Body, h.price, w.settler.cfg.Estimate)
		if err != nil {
			return out, 0, err
		}
	}
	actual, err := w.settler.settlementPrice(h, out)
	if err != nil {
		return out, 0, err
	}
	return out, actual, nil
}

func (w *WorkflowSettler) committedActual(ctx context.Context, h *hold, status string) (credits.Micro, bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, w.settler.cfg.RedisTimeout)
	defer cancel()
	value, err := w.settler.rdb.Get(callCtx, h.keys.done).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("%w: task completion lookup: %v", gateway.ErrBillingUnavailable, err)
	}
	amount, err := strconv.ParseInt(value, 10, 64)
	if err != nil || amount < 0 || status == "failed" && amount != 0 {
		return 0, true, errors.New("billing: conflicting task finalization")
	}
	return credits.Micro(amount), true, nil
}

func emptyWorkflowUsage(u gateway.Usage) bool {
	return u.PromptTokens == 0 && u.CompletionTokens == 0 && u.CachedTokens == 0 && u.CacheWriteTokens == 0 &&
		u.CacheWrite1hTokens == 0 && u.ImageInputTokens == 0 && u.ImageOutputTokens == 0 && u.AudioInputTokens == 0 &&
		u.AudioOutputTokens == 0 && u.ImageCount == 0 && u.AudioDurationMicros == 0 && u.AudioCharacters == 0 &&
		u.VideoDurationMicros == 0 && len(u.ToolCalls) == 0
}
