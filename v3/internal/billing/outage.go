package billing

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// reserveLocal admits a request from this process's outage allowance.
// Nothing is held in Redis; the charge reaches Redis through the WAL.
func (s *Settler) reserveLocal(req *gateway.Request, account int64, amount credits.Micro, k keys) error {
	if s.cfg.DisableOutageAdmission {
		s.stats.Refused.Add(1)
		return ErrBillingDegraded
	}
	if err := s.local.take(account, amount); err != nil {
		s.stats.Refused.Add(1)
		return err
	}
	s.stats.LocalReserves.Add(1)
	req.Reserve = &hold{account: account, amount: amount, keys: k, local: true}
	return nil
}

// finalizeLocal makes the settlement durable in the WAL. For a local hold the
// estimate committed at reserve is corrected to the actual charge; for a hold
// made in Redis before the outage, the actual charge is new local spending.
func (s *Settler) finalizeLocal(h *hold, rec walRecord, actual credits.Micro) error {
	if err := s.wal.append(rec); err != nil {
		return fmt.Errorf("billing: outage wal for account %d: %w", h.account, err)
	}
	s.stats.LocalFinalizes.Add(1)
	if h.local {
		s.local.adjust(h.account, actual-h.amount)
	} else {
		s.local.adjust(h.account, actual)
	}
	return nil
}

// Stats returns outage-mode counters and the number of WAL records waiting
// for replay.
func (s *Settler) Stats() (localReserves, localFinalizes, refused, replayed, walPending int64) {
	if s.wal != nil {
		walPending = s.wal.pending.Load()
	}
	return s.stats.LocalReserves.Load(), s.stats.LocalFinalizes.Load(), s.stats.Refused.Load(), s.stats.Replayed.Load(), walPending
}

// Run replays the WAL into Redis whenever Redis is reachable, until ctx is
// canceled. It also replays segments left by a previous process.
func (s *Settler) Run(ctx context.Context) error {
	if s.wal == nil {
		<-ctx.Done()
		return nil
	}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		if s.wal.pending.Load() == 0 || s.br.open() {
			continue
		}
		if err := s.Replay(ctx); err != nil {
			s.log.Warn("billing: wal replay paused", "err", err, "pending", s.wal.pending.Load())
		}
	}
}

// Replay applies every sealed WAL segment to Redis, oldest first, deleting
// each once fully applied. It stops at the first failure and resumes from the
// start of that segment next time; records already applied are no-ops then.
func (s *Settler) Replay(ctx context.Context) error {
	segs, err := s.wal.sealed()
	if err != nil {
		return err
	}
	for _, seg := range segs {
		recs, err := readSegment(seg.path)
		if err != nil {
			return err // corrupt segment: keep it for an operator
		}
		for _, rec := range recs {
			if _, err := s.runFinalize(ctx, rec); err != nil {
				if isOutage(ctx, err) {
					s.br.fail()
				}
				return fmt.Errorf("billing: replay %s: %w", seg.path, err)
			}
			s.stats.Replayed.Add(1)
		}
		if err := os.Remove(seg.path); err != nil {
			return fmt.Errorf("billing: remove replayed segment: %w", err)
		}
		if s.wal.pending.Add(-int64(len(recs))) == 0 {
			s.local.resetSpent()
		}
		s.log.Info("billing: replayed outage settlements", "segment", seg.path, "records", len(recs))
	}
	s.br.ok()
	return nil
}

// Close flushes and closes the WAL. Unreplayed records stay on disk and are
// replayed by the next process using the same WALDir.
func (s *Settler) Close() {
	if s.wal != nil {
		s.wal.close()
	}
}
