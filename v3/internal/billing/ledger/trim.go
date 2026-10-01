package ledger

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// Trim drops stream entries that every consumer group has acknowledged.
// For each group the safe boundary is its oldest pending entry, or, with
// nothing pending, everything up to its last delivered entry. The stream is
// trimmed to the minimum over all groups, so a future audit consumer group is
// never cut short by the ledger group's progress.
func (w *Worker) Trim(ctx context.Context) error {
	groups, err := w.rdb.XInfoGroups(ctx, redisx.StreamBillingEvents).Result()
	if err != nil {
		return fmt.Errorf("ledger: stream groups: %w", err)
	}
	if len(groups) == 0 {
		return nil
	}
	var floor string
	for _, g := range groups {
		boundary := nextID(g.LastDeliveredID)
		if g.Pending > 0 {
			p, err := w.rdb.XPending(ctx, redisx.StreamBillingEvents, g.Name).Result()
			if err != nil {
				return fmt.Errorf("ledger: pending of %s: %w", g.Name, err)
			}
			boundary = p.Lower
		}
		if floor == "" || lessID(boundary, floor) {
			floor = boundary
		}
	}
	if floor == "" || floor == "0-0" {
		return nil
	}
	// MINID removes entries with ids strictly below floor.
	return w.rdb.XTrimMinID(ctx, redisx.StreamBillingEvents, floor).Err()
}

// nextID returns the smallest stream id greater than id.
func nextID(id string) string {
	ms, seq, ok := splitID(id)
	if !ok {
		return "0-0"
	}
	return strconv.FormatUint(ms, 10) + "-" + strconv.FormatUint(seq+1, 10)
}

func lessID(a, b string) bool {
	am, as, _ := splitID(a)
	bm, bs, _ := splitID(b)
	return am < bm || (am == bm && as < bs)
}

func splitID(id string) (ms, seq uint64, ok bool) {
	msPart, seqPart, found := strings.Cut(id, "-")
	if !found {
		return 0, 0, false
	}
	ms, err1 := strconv.ParseUint(msPart, 10, 64)
	seq, err2 := strconv.ParseUint(seqPart, 10, 64)
	return ms, seq, err1 == nil && err2 == nil
}
