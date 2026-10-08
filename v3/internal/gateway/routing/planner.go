// Package routing turns a catalog snapshot into an ordered RoutePlan per
// request and learns from attempt results (plan §3, scheduler design borrowed
// from CLIProxyAPI).
//
// A plan is computed once per request. Reports only affect later plans, never
// one that was already returned, so a retry uses the same decision as the
// first attempt (v2 regression 8444c4dbf).
package routing

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// ErrNoRoute means no enabled channel serves the group and model.
var ErrNoRoute = errors.New("routing: no route for group and model")

// Config tunes the planner. Zero values select defaults.
type Config struct {
	MaxAttempts     int           // candidates per plan, default 3
	BaseBackoff     time.Duration // first cooldown without Retry-After, default 1 s
	MaxBackoff      time.Duration // default 30 min
	AuthCooldown    time.Duration // floor for 401/403/402, default 30 min
	MaxRetryAfter   time.Duration // cap on upstream Retry-After hints, default 24 h
	AffinityEntries int           // default 100k
	AffinityTTL     time.Duration // default 1 h
	Now             func() time.Time
}

func (c Config) withDefaults() Config {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Minute
	}
	if c.AuthCooldown <= 0 {
		c.AuthCooldown = 30 * time.Minute
	}
	if c.MaxRetryAfter <= 0 {
		c.MaxRetryAfter = 24 * time.Hour
	}
	if c.AffinityEntries <= 0 {
		c.AffinityEntries = 100_000
	}
	if c.AffinityTTL <= 0 {
		c.AffinityTTL = time.Hour
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Planner implements gateway.Planner.
type Planner struct {
	cfg      Config
	snapshot func() *catalog.Snapshot
	cool     *cooldowns
	aff      *affinity
	official *officialHealth
	indexes  atomic.Pointer[indexCache]
}

// indexCache holds route indexes for exactly one snapshot.
type indexCache struct {
	snap *catalog.Snapshot
	m    sync.Map // routeKey -> *routeIndex
}

type routeKey struct{ group, model string }

// New returns a planner reading the current snapshot from snapshot().
func New(snapshot func() *catalog.Snapshot, cfg Config) *Planner {
	cfg = cfg.withDefaults()
	return &Planner{
		cfg:      cfg,
		snapshot: snapshot,
		cool:     newCooldowns(),
		aff:      newAffinity(cfg.AffinityEntries, int64(cfg.AffinityTTL)),
		official: newOfficialHealth(),
	}
}

// Plan returns up to MaxAttempts distinct credentials, best first.
func (p *Planner) Plan(_ context.Context, req *gateway.Request) ([]gateway.Target, error) {
	snap := p.snapshot()
	if snap == nil {
		return nil, ErrNoRoute
	}
	b := planBuilder{p: p, snap: snap, req: req, now: p.cfg.Now().UnixNano(), max: p.cfg.MaxAttempts}
	b.targets = make([]gateway.Target, 0, b.max)

	sessionKey, hasSession := p.aff.sessionKey(req)
	groups := requestedGroups(req, snap)
	if req.Principal.Group == "zero-hour" || req.Principal.Group == "monthly-pass" {
		groups = []string{cardRouteGroup(snap)}
	}
	for _, group := range groups {
		p.planGroup(&b, group, sessionKey, hasSession)
		if b.full() {
			break
		}
	}
	if len(b.targets) == 0 {
		if !configuredModel(snap, req.Model) {
			return nil, errors.Join(ErrNoRoute, &gateway.UpstreamError{
				Status: http.StatusNotFound, Type: "invalid_request_error", Code: "model_not_found",
				Message: "model does not exist or is not available to this API key",
			})
		}
		return nil, ErrNoRoute
	}
	if hasSession && len(b.targets) > 0 {
		// Recorded optimistically; a failure cools the credential, which makes
		// the next plan skip it and record the replacement.
		p.aff.put(sessionKey, b.targets[0].CredentialID, b.now)
	}
	return b.targets, nil
}

// No targets for a configured model can be transient; an unknown name cannot
// become routable by repeating the same request. Inspect the immutable catalog.
func configuredModel(snap *catalog.Snapshot, model string) bool {
	if _, ok := snap.Prices[model]; ok {
		return true
	}
	for _, routes := range snap.Routes {
		if _, ok := routes[model]; ok {
			return true
		}
	}
	for _, policy := range snap.Market.Channels {
		if _, ok := policy.ModelPrices[model]; ok {
			return true
		}
	}
	return false
}

// planGroup fills b with candidates for one requested group: it resolves the
// group's route index (pool, official pool, or plain index), tries session
// affinity, then fills by score/priority with a card-fallback retry when the
// strict pass left b short and the principal isn't zero-hour locked.
func (p *Planner) planGroup(b *planBuilder, group string, sessionKey uint64, hasSession bool) {
	req, snap := b.req, b.snap
	b.cardFallback = false
	b.poolOrdered, b.poolGroups, b.group = false, nil, group
	b.officialSelections = nil
	if pool, ok := snap.Market.Pools[group]; ok && len(pool.Members) > 0 && (pool.Strategy == "priority" || pool.Strategy == "cost" || pool.Strategy == "score") {
		b.idx = b.orderedPoolIndex(pool)
	} else if pool, ok := snap.OfficialPools[group]; ok && pool.Matches(req.Model) {
		b.idx, b.officialSelections = officialPoolIndex(snap, pool, req.Model, p.official, time.Unix(0, b.now))
		b.poolOrdered = true
		b.enableOfficialIsolation()
	} else {
		b.idx = p.index(snap, group, req.Model)
	}
	if b.idx == nil || b.idx.size == 0 {
		return
	}
	if pool, ok := snap.Market.Pools[group]; ok && pool.MaxAttempts > 0 {
		b.max = min(p.cfg.MaxAttempts, pool.MaxAttempts)
	}
	if hasSession {
		b.tryAffinity(sessionKey)
	}
	b.fill()
	if len(b.targets) == 0 {
		b.fallbackCooling()
	}
	// Ordinary card requests may retry an ineligible channel at full price.
	// Explicit zero-hour requests must remain within their card policy.
	if !b.full() && req.Principal.Group != "zero-hour" {
		b.cardFallback = true
		b.fill()
		if len(b.targets) == 0 {
			b.fallbackCooling()
		}
	}
}

// index returns the cached candidate set, building it on first use per snapshot.
func (p *Planner) index(snap *catalog.Snapshot, group, model string) *routeIndex {
	cache := p.indexes.Load()
	if cache == nil || cache.snap != snap {
		fresh := &indexCache{snap: snap}
		if p.indexes.CompareAndSwap(cache, fresh) {
			cache = fresh
		} else {
			cache = p.indexes.Load()
			if cache.snap != snap { // another snapshot won the race; don't cache
				return p.build(snap, group, model)
			}
		}
	}
	key := routeKey{group, model}
	if v, ok := cache.m.Load(key); ok {
		return v.(*routeIndex)
	}
	v, _ := cache.m.LoadOrStore(key, p.build(snap, group, model))
	return v.(*routeIndex)
}

func (p *Planner) build(snap *catalog.Snapshot, group, model string) *routeIndex {
	byModel := snap.Routes[group]
	routes, ok := byModel[model]
	if !ok {
		routes = byModel["*"]
	}
	if len(routes) == 0 {
		return nil
	}
	return buildIndex(snap, routes)
}

// Report feeds an attempt result back into cooldowns.
func (p *Planner) Report(target gateway.Target, res gateway.AttemptResult) {
	p.official.observe(target, res, OfficialAttemptMetrics{TTFT: res.TTFT, PromptTokens: res.PromptTokens, CachedTokens: res.CachedTokens}, p.cfg.Now())
	credKey := coolKey{cred: target.CredentialID}
	modelKey := coolKey{cred: target.CredentialID, model: target.UpstreamModel}
	if res.OK {
		p.cool.success(credKey)
		p.cool.success(modelKey)
		p.cool.success(coolKey{cred: target.CredentialID, hard: true})
		p.cool.success(coolKey{cred: target.CredentialID, model: target.UpstreamModel, hard: true})
		return
	}
	now := p.cfg.Now().UnixNano()
	duration := func(streak int) time.Duration { return p.cooldownFor(res, streak) }
	switch res.Scope {
	case gateway.ScopeCredential:
		p.cool.fail(credKey, now, duration)
		if authFailure(res.Status) {
			p.cool.fail(coolKey{cred: target.CredentialID, hard: true}, now, duration)
		}
	case gateway.ScopeModel:
		p.cool.fail(modelKey, now, duration)
		if authFailure(res.Status) || res.Status == http.StatusNotFound {
			p.cool.fail(coolKey{cred: target.CredentialID, model: target.UpstreamModel, hard: true}, now, duration)
		}
	default:
		return // request errors must not poison a user's other requests
	}
	if res.Retryable && target.PersonalPoolGroup != "" && target.PoolFailureCooldown > 0 {
		key := coolKey{cred: target.ChannelID, model: target.UpstreamModel, pool: target.PersonalPoolGroup}
		p.cool.fail(key, now, func(int) time.Duration { return target.PoolFailureCooldown })
	}
}

func authFailure(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusPaymentRequired
}

func (p *Planner) cooldownFor(res gateway.AttemptResult, streak int) time.Duration {
	d := p.cfg.BaseBackoff
	for i := 1; i < streak && d < p.cfg.MaxBackoff; i++ {
		d *= 2
	}
	d = min(d, p.cfg.MaxBackoff)
	if res.RetryAfter > 0 {
		d = min(res.RetryAfter, p.cfg.MaxRetryAfter)
	}
	switch res.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusPaymentRequired:
		d = max(d, p.cfg.AuthCooldown)
	}
	return d
}
