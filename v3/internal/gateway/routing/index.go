package routing

import (
	"math/rand/v2"
	"sort"
	"sync/atomic"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// routeIndex is the precomputed candidate set for one (group, model) on one
// snapshot. It is built once and shared by every Plan until the next snapshot.
type routeIndex struct {
	tiers []*tier           // priority descending
	creds map[int64]credLoc // credential id -> position, for affinity lookups
	size  int               // total credentials
}

// tier holds the channels of one priority level.
type tier struct {
	priority int
	strategy string
	entries  []*chanEntry
	prefix   []int64 // cumulative weights, for O(log n) weighted sampling
	total    int64
	rr       atomic.Uint64
}

type chanEntry struct {
	ch       *catalog.Channel
	priority int
	next     atomic.Uint32 // rotates credentials within the channel
}

type credLoc struct {
	entry *chanEntry
	pos   int
}

func buildIndex(snap *catalog.Snapshot, routes []catalog.Route) *routeIndex {
	sorted := append([]catalog.Route(nil), routes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ChannelID < sorted[j].ChannelID })

	idx := &routeIndex{creds: make(map[int64]credLoc)}
	byPriority := make(map[int]*tier)
	for _, r := range sorted {
		ch := snap.Channels[r.ChannelID]
		if ch == nil || len(ch.Credentials) == 0 {
			continue
		}
		t := byPriority[r.Priority]
		if t == nil {
			t = &tier{priority: r.Priority, strategy: r.Strategy}
			byPriority[r.Priority] = t
		}
		e := &chanEntry{ch: ch, priority: r.Priority}
		t.entries = append(t.entries, e)
		t.total += int64(max(r.Weight, 1))
		t.prefix = append(t.prefix, t.total)
		for pos, c := range ch.Credentials {
			idx.creds[c.ID] = credLoc{entry: e, pos: pos}
		}
		idx.size += len(ch.Credentials)
	}
	for _, t := range byPriority {
		idx.tiers = append(idx.tiers, t)
	}
	sort.Slice(idx.tiers, func(i, j int) bool { return idx.tiers[i].priority > idx.tiers[j].priority })
	return idx
}

// sample draws one channel with probability proportional to its weight.
func (t *tier) sample() *chanEntry {
	x := rand.Int64N(t.total)
	i := sort.Search(len(t.prefix), func(i int) bool { return t.prefix[i] > x })
	return t.entries[i]
}
