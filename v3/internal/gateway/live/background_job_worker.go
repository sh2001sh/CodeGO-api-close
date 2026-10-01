package live

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const (
	backgroundLeaseTTL    = 30 * time.Second
	backgroundConcurrency = 32
)

var (
	errBackgroundCanceled = errors.New("live: background cancellation requested")
	errBackgroundUnknown  = errors.New("live: background upstream acceptance is unknown")
)

func backgroundTerminal(status string) bool {
	return status == "completed" || status == "incomplete" || status == "failed" || status == "cancelled"
}

func (h *Handler) backgroundReady() bool {
	return h.cfg.BackgroundJobs != nil && h.cfg.BackgroundBilling != nil && h.cfg.ResolvePrincipal != nil
}

// Run polls durable work; request clients can disconnect without canceling jobs.
func (h *Handler) Run(ctx context.Context) error {
	if !h.backgroundReady() {
		return errors.New("live: durable background dependencies are required")
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var workers sync.WaitGroup
	dispatch := func() {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := h.Reconcile(ctx, 32); err != nil && ctx.Err() == nil {
				h.cfg.Logger.Warn("background reconciliation failed", "err", err)
			}
		}()
	}
	dispatch()
	for {
		select {
		case <-ctx.Done():
			workers.Wait()
			return ctx.Err()
		case <-ticker.C:
			dispatch()
		}
	}
}

func (h *Handler) Reconcile(ctx context.Context, limit int) error {
	if !h.backgroundReady() {
		return errors.New("live: durable background dependencies are required")
	}
	if limit <= 0 {
		return nil
	}
	h.backgroundSlotsOnce.Do(func() { h.backgroundSlots = make(chan struct{}, backgroundConcurrency) })
	limit = min(limit, backgroundConcurrency)
	ids, err := h.cfg.BackgroundJobs.Pending(ctx, limit)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []error
dispatch:
	for _, id := range ids {
		if ctx.Err() != nil {
			mu.Lock()
			failures = append(failures, ctx.Err())
			mu.Unlock()
			break
		}
		// Admission precedes the lease, so queued work never waits behind a
		// process slot while its durable claim expires.
		select {
		case h.backgroundSlots <- struct{}{}:
		default:
			break dispatch
		}
		job, err := h.cfg.BackgroundJobs.Claim(ctx, id, requestID(), backgroundLeaseTTL)
		if errors.Is(err, ErrBackgroundLeaseConflict) || errors.Is(err, ErrNotFound) {
			<-h.backgroundSlots
			continue
		}
		if err != nil {
			<-h.backgroundSlots
			mu.Lock()
			failures = append(failures, err)
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-h.backgroundSlots }()
			if err := h.executeBackgroundJob(ctx, job); err != nil {
				mu.Lock()
				failures = append(failures, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(failures...)
}

func backgroundFailure(code string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: 502, Type: "upstream_error", Code: code, Message: "background execution failed"}
}
