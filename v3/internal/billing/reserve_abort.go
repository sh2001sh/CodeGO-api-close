package billing

import (
	"context"
	"fmt"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// A lost reserve reply can mean the hold committed, or the command may still
// execute after Redis recovers. A zero settlement both releases any hold and
// installs the existing done fence against that late command. This runs only
// for rejected admission; successfully admitted work retains its own hold.
func (s *Settler) rejectReserve(ctx context.Context, req *gateway.Request, h *hold, cause error) error {
	abort := *req
	abort.Model = "" // releases carry no billable usage or marketplace evidence
	out := gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput}
	rec := s.finalizeCall(&abort, out, h, 0)
	if len(h.funding) > 0 {
		var err error
		rec, err = s.fundingFinalizeCall(&abort, out, h, 0)
		if err != nil {
			return fmt.Errorf("%w: reserve failed: %v; prepare cancellation: %v", gateway.ErrBillingUnavailable, cause, err)
		}
	}
	if s.wal != nil {
		if err := s.wal.append(rec); err != nil {
			return fmt.Errorf("%w: reserve failed: %v; durable cancellation: %v", gateway.ErrBillingUnavailable, cause, err)
		}
	} else if _, err := s.runFinalize(context.WithoutCancel(ctx), rec); err != nil {
		return fmt.Errorf("%w: reserve failed: %v; cancellation unavailable: %v", gateway.ErrBillingUnavailable, cause, err)
	}
	return fmt.Errorf("%w: reserve failed: %v", gateway.ErrBillingUnavailable, cause)
}
