package identity

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// Invalidate applies one outbox payload ("api_key:<id>" or "user:<id>") to
// L1 and L2. Other entities are ignored. It is safe to call from every
// instance for the same payload.
func (a *Authorizer) Invalidate(ctx context.Context, payload string) error {
	entity, rawID, ok := strings.Cut(payload, ":")
	if !ok {
		return nil
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return nil // not an identity payload
	}
	// Order matters: clear L2 first, then fence and clear L1. A fetch that
	// read the old L2 entry either fills L1 before the epoch bump (and is
	// removed right after) or after it (and is rejected). Clearing L1 first
	// would let a fetch in the gap re-cache the stale L2 entry.
	var l2err error
	var hashes []string
	switch entity {
	case "api_key":
		l2err = a.l2.invalidateKey(ctx, id)
		if hs, ok := a.idx.key(id); ok {
			hashes = []string{hs}
		}
	case "user":
		l2err = a.l2.invalidateUser(ctx, id)
		hashes = a.idx.user(id)
	default:
		return nil
	}
	a.epoch.Add(1)
	for _, hs := range hashes {
		a.l1.remove(hs)
	}
	if entity == "user" { // keys cached after idx.user() was read
		for _, hs := range a.idx.user(id) {
			a.l1.remove(hs)
		}
	}
	return l2err
}

// Ready is closed once Run's subscription is active. A gateway must wait for
// it before serving traffic; invalidations published earlier are not seen.
func (a *Authorizer) Ready() <-chan struct{} { return a.ready }

// Run applies invalidations from redisx.ChannelInvalidate and flushes
// last_used_at writes until ctx is canceled. If pub/sub drops messages while
// reconnecting, L1TTL bounds how long a stale entry can survive.
func (a *Authorizer) Run(ctx context.Context) error {
	if a.touch != nil {
		go a.touch.run(ctx, a.cfg.TouchWindow/3)
	}
	sub := a.rdb.Subscribe(ctx, redisx.ChannelInvalidate)
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(ctx); err != nil { // wait for the subscription to be active
		return err
	}
	a.readyOnce.Do(func() { close(a.ready) })
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-sub.Channel():
			if !ok {
				return nil
			}
			invCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			if err := a.Invalidate(invCtx, msg.Payload); err != nil {
				a.log.Error("identity: invalidation failed", "payload", msg.Payload, "err", err)
			}
			cancel()
		}
	}
}
