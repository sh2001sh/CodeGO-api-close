package routing

import (
	"sort"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
)

// weightedSamples bounds random draws per slot before falling back to a scan,
// which is only reached when most of a tier is cooling down.
const weightedSamples = 8

// planBuilder assembles one plan. It is single-use and never shared.
type planBuilder struct {
	p                   *Planner
	snap                *catalog.Snapshot
	req                 *gateway.Request
	group               string
	cardFallback        bool
	poolOrdered         bool
	poolGroups          map[int64]string
	officialSelections  map[int64]OfficialSelection
	officialUsedDomains map[string]bool
	idx                 *routeIndex
	now                 int64
	max                 int
	targets             []gateway.Target
}

func (b *planBuilder) full() bool { return len(b.targets) >= b.max }

func (b *planBuilder) has(credID int64) bool {
	for i := range b.targets {
		if b.targets[i].CredentialID == credID {
			return true
		}
	}
	return false
}

// cooling reports when a credential becomes usable for this model (0 = now).
func (b *planBuilder) cooling(ch *catalog.Channel, credID int64) int64 {
	model := ch.UpstreamModel(b.req.Model)
	return max(b.p.cool.until(coolKey{cred: credID}, b.now), b.p.cool.until(coolKey{cred: credID, model: model}, b.now), b.hardCooling(ch, credID))
}

// Configured pool cooldowns and auth/model-unavailable failures are hard exclusions.
// The all-cooling availability probe only bypasses ordinary global backoff.
func (b *planBuilder) hardCooling(ch *catalog.Channel, credID int64) int64 {
	model := ch.UpstreamModel(b.req.Model)
	until := max(b.p.cool.until(coolKey{cred: credID, hard: true}, b.now), b.p.cool.until(coolKey{cred: credID, model: model, hard: true}, b.now))
	if pool, ok := b.snap.Market.Pools[b.group]; ok && pool.FailureCooldownSeconds > 0 {
		until = max(until, b.p.cool.until(coolKey{cred: ch.ID, model: model, pool: b.group}, b.now))
	}
	return until
}

func (b *planBuilder) add(ch *catalog.Channel, cred catalog.Credential) {
	group, factor := b.targetPricing(ch)
	integerFactor, _ := exactfactor.Int64(factor)
	poolGroup, poolCooldown := "", time.Duration(0)
	if pool, ok := b.snap.Market.Pools[b.group]; ok {
		poolGroup, poolCooldown = b.group, time.Duration(pool.FailureCooldownSeconds)*time.Second
	}
	selection := b.officialSelections[ch.ID]
	if b.officialUsedDomains != nil {
		b.officialUsedDomains[b.officialDomain(ch)] = true
	}
	b.targets = append(b.targets, gateway.Target{
		ChannelID:                ch.ID,
		CredentialID:             cred.ID,
		Provider:                 ch.Provider,
		BaseURL:                  ch.BaseURL,
		Secret:                   cred.Secret,
		UpstreamModel:            ch.UpstreamModel(b.req.Model),
		ProxyURL:                 ch.ProxyURL,
		MaxConcurrency:           ch.MaxConcurrency,
		MaxUserConcurrency:       ch.MaxUserConcurrency,
		CredentialMaxConcurrency: cred.MaxConcurrency,
		Settings:                 ch.Settings, ParamOverride: ch.ParamOverride,
		HeaderOverride: ch.HeaderOverride, StatusCodeMapping: ch.StatusCodeMapping,
		Fingerprint: gateway.CredentialFingerprint{UserAgent: cred.Fingerprint.UserAgent, TLSProfile: cred.Fingerprint.TLSProfile},
		Scope:       ch.Scope, OwnerUserID: ch.OwnerUserID, Group: group, MultiplierPPM: integerFactor, MultiplierPPMExact: factor,
		RoutePoolID: selection.PoolID, ProcurementCostMultiplier: selection.CostMultiplier,
		PersonalPoolGroup: poolGroup, PoolFailureCooldown: poolCooldown,
	})
}

func (b *planBuilder) tryAffinity(sessionKey uint64) {
	credID, ok := b.p.aff.get(sessionKey, b.now)
	if !ok {
		return
	}
	loc, ok := b.idx.creds[credID]
	if !ok { // credential left this route since the session started
		return
	}
	// Pool strategies order groups before credential affinity, as the source
	// distributor does. Affinity may still choose a key in the first group.
	if b.poolOrdered && loc.entry.priority != b.idx.tiers[0].priority {
		return
	}
	if b.usable(loc.entry.ch, loc.entry.ch.Credentials[loc.pos]) && b.cooling(loc.entry.ch, credID) == 0 && !b.has(credID) {
		b.add(loc.entry.ch, loc.entry.ch.Credentials[loc.pos])
	}
}

// fill walks tiers by priority. Within a tier it first spreads the plan over
// distinct channels (a failure is often channel-wide), then allows further
// keys of channels already chosen.
func (b *planBuilder) fill() {
	for _, t := range b.idx.tiers {
		if b.full() {
			return
		}
		if t.strategy == "weighted" || t.strategy == "" {
			b.fillWeighted(t)
		}
		start := 0
		if t.strategy != "fill_first" {
			start = int(t.rr.Add(1) % uint64(len(t.entries)))
		}
		for _, distinct := range [2]bool{true, false} {
			for i := 0; i < len(t.entries) && !b.full(); i++ {
				b.takeFrom(t.entries[(start+i)%len(t.entries)], distinct)
			}
		}
	}
}

func (b *planBuilder) fillWeighted(t *tier) {
	for draws := 0; draws < weightedSamples*b.max && !b.full(); draws++ {
		b.takeFrom(t.sample(), true)
	}
}

func (b *planBuilder) hasChannel(id int64) bool {
	for i := range b.targets {
		if b.targets[i].ChannelID == id {
			return true
		}
	}
	return false
}

// takeFrom adds the channel's next usable credential, rotating so load spreads
// across a channel's keys. With distinct set it skips channels already planned.
func (b *planBuilder) takeFrom(e *chanEntry, distinct bool) {
	if distinct && b.hasChannel(e.ch.ID) {
		return
	}
	creds := e.ch.Credentials
	start := int(e.next.Add(1) % uint32(len(creds)))
	for i := range creds {
		c := creds[(start+i)%len(creds)]
		if !b.has(c.ID) && b.usable(e.ch, c) && b.cooling(e.ch, c.ID) == 0 {
			b.add(e.ch, c)
			return
		}
	}
}

// fallbackCooling runs when every candidate is cooling: rather than a certain
// 503, try the ones that recover soonest.
func (b *planBuilder) fallbackCooling() {
	type cand struct {
		ch    *catalog.Channel
		cred  catalog.Credential
		until int64
	}
	var all []cand
	for _, t := range b.idx.tiers {
		for _, e := range t.entries {
			for _, c := range e.ch.Credentials {
				if !b.has(c.ID) && b.usable(e.ch, c) && b.hardCooling(e.ch, c.ID) == 0 {
					all = append(all, cand{e.ch, c, b.cooling(e.ch, c.ID)})
				}
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].until < all[j].until })
	for i := 0; i < len(all) && !b.full(); i++ {
		if b.usable(all[i].ch, all[i].cred) {
			b.add(all[i].ch, all[i].cred)
		}
	}
}
