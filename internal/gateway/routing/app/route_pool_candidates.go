package app

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

const routeHealthRequestCacheKey = "gateway_route_health_request_cache"

type routeHealthCacheEntry struct {
	health gatewayruntime.ChannelHealth
	found  bool
}

type routeHealthRequestCache struct {
	channels map[string]routeHealthCacheEntry
	domains  map[string]routeHealthCacheEntry
	prefetch *routeHealthPrefetch
}

// A round owns its snapshot. Reuse its maps instead of copying every large
// ChannelHealth into another per-candidate cache entry; membership sets also
// retain negative lookups without treating unrequested IDs as cache misses.
type routeHealthPrefetch struct {
	model       string
	requestType gatewayruntime.RequestType
	channels    map[int]struct{}
	domains     map[string]struct{}
	snapshot    gatewayruntime.RouteHealthSnapshot
}

// resetRouteHealthRequestCache starts a fresh advisory-health view for a new
// routing round. Retries can update circuit state, so cached values must not
// leak across retry rounds.
func resetRouteHealthRequestCache(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(routeHealthRequestCacheKey, &routeHealthRequestCache{
		channels: make(map[string]routeHealthCacheEntry),
		domains:  make(map[string]routeHealthCacheEntry),
	})
}

func getRouteHealthRequestCache(c *gin.Context) *routeHealthRequestCache {
	if c == nil {
		return nil
	}
	if value, exists := c.Get(routeHealthRequestCacheKey); exists {
		if cache, ok := value.(*routeHealthRequestCache); ok && cache != nil {
			return cache
		}
	}
	cache := &routeHealthRequestCache{
		channels: make(map[string]routeHealthCacheEntry),
		domains:  make(map[string]routeHealthCacheEntry),
	}
	c.Set(routeHealthRequestCacheKey, cache)
	return cache
}

type routePoolCandidateClass uint8

const (
	routePoolCandidateSkip routePoolCandidateClass = iota
	routePoolCandidateHealthy
	routePoolCandidateProbe
	routePoolCandidateLastResort
)

func buildRoutePoolCandidateSets(
	c *gin.Context,
	candidates []gatewaystore.RoutePoolCandidate,
	modelName string,
	requestType gatewayruntime.RequestType,
	now time.Time, pools ...gatewayschema.RoutePool,
) (healthy, probes, lastResort []scoredRoutePoolCandidate) {
	prefetchRoutePoolHealth(c, candidates, modelName, requestType)
	healthy = make([]scoredRoutePoolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		var pool *gatewayschema.RoutePool
		if len(pools) > 0 {
			pool = &pools[0]
		}
		scored, class := classifyRoutePoolCandidate(c, candidate, modelName, requestType, now, pool)
		switch class {
		case routePoolCandidateHealthy:
			healthy = append(healthy, scored)
		case routePoolCandidateProbe:
			probes = append(probes, scored)
		case routePoolCandidateLastResort:
			lastResort = append(lastResort, scored)
		}
	}
	return healthy, probes, lastResort
}

func preferRemoteCompactionCandidates(
	healthy, probes, lastResort []scoredRoutePoolCandidate,
) ([]scoredRoutePoolCandidate, []scoredRoutePoolCandidate, []scoredRoutePoolCandidate) {
	if len(healthy)+len(probes)+len(lastResort) == 0 {
		return healthy, probes, lastResort
	}
	hasSupported := func(candidates []scoredRoutePoolCandidate) bool {
		for _, candidate := range candidates {
			if candidate.compactionCapabilityRank == 0 {
				return true
			}
		}
		return false
	}
	if hasSupported(healthy) || hasSupported(probes) || hasSupported(lastResort) {
		filter := func(candidates []scoredRoutePoolCandidate) []scoredRoutePoolCandidate {
			filtered := make([]scoredRoutePoolCandidate, 0, len(candidates))
			for _, candidate := range candidates {
				if candidate.compactionCapabilityRank == 0 {
					filtered = append(filtered, candidate)
				}
			}
			return filtered
		}
		return filter(healthy), filter(probes), filter(lastResort)
	}
	return healthy, probes, lastResort
}

func classifyRoutePoolCandidate(
	c *gin.Context,
	candidate gatewaystore.RoutePoolCandidate,
	modelName string,
	requestType gatewayruntime.RequestType,
	now time.Time, pool *gatewayschema.RoutePool,
) (scoredRoutePoolCandidate, routePoolCandidateClass) {
	if pool != nil && strings.TrimSpace(pool.ModelScope) != "" && !strings.EqualFold(strings.TrimSpace(pool.ModelScope), strings.TrimSpace(modelName)) {
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	if channelExcludedByScope(c, candidate.Channel) {
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "non_official_channel")
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	if channelAlreadyUsed(c, candidate.Channel.Id) {
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	compactionRank := remoteCompactionCapabilityRank(c, candidate.Channel, modelName)
	if compactionRank < 0 {
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "remote_compaction_unsupported")
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	faultDomain := routePoolFaultDomain(candidate.Member, candidate.Channel)
	if gatewayruntime.IsFaultDomainExcluded(c, faultDomain) {
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "failed_fault_domain")
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	credentialHealth, credentialFound := routePoolCredentialHealth(c, candidate.Channel.Id)
	credentialCircuit := credentialFound && (credentialHealth.State == gatewayruntime.ChannelHealthCooling || credentialHealth.State == gatewayruntime.ChannelHealthHalfOpen)
	if credentialCircuit && (credentialHealth.CoolingUntil.After(now) || credentialHealth.RecoveryProbeUntil.After(now)) {
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "channel_credential_cooling")
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}

	health, found := routePoolChannelHealth(c, candidate.Channel.Id, modelName, requestType)
	domainHealth, domainFound := routePoolFaultDomainHealth(c, faultDomain, modelName, requestType)
	channelCooling := activeRoutePoolCircuit(health, found, now)
	domainCooling := activeRoutePoolCircuit(domainHealth, domainFound, now)
	cost := routePoolModelCost(candidate.Member, modelName)
	score := effectiveRoutePoolCostValueForPool(cost, health, pool)
	if compactionRank > 0 {
		score += routePoolUnknownCompactionPenalty
	}
	scored := scoredRoutePoolCandidate{
		channel: candidate.Channel, score: score, compactionCapabilityRank: compactionRank,
		cost: cost, health: health, faultDomain: faultDomain,
	}
	if channelCooling || domainCooling {
		scored.channelProbe = channelCooling
		scored.domainProbe = domainCooling
		return scored, routePoolCandidateLastResort
	}

	scored.credentialProbe = credentialCircuit && !credentialHealth.CoolingUntil.After(now) && !credentialHealth.RecoveryProbeUntil.After(now)
	scored.channelProbe = found && (health.State == gatewayruntime.ChannelHealthCooling || health.State == gatewayruntime.ChannelHealthHalfOpen)
	scored.domainProbe = domainFound && (domainHealth.State == gatewayruntime.ChannelHealthCooling || domainHealth.State == gatewayruntime.ChannelHealthHalfOpen)
	if scored.channelProbe || scored.credentialProbe || scored.domainProbe {
		scored.probe = true
		return scored, routePoolCandidateProbe
	}
	return scored, routePoolCandidateHealthy
}

func prefetchRoutePoolHealth(c *gin.Context, candidates []gatewaystore.RoutePoolCandidate, model string, requestType gatewayruntime.RequestType) {
	if c == nil || len(candidates) == 0 {
		return
	}
	ids := make([]int, 0, len(candidates))
	domains := make([]string, 0, len(candidates))
	channelSet := make(map[int]struct{}, len(candidates))
	domainSet := make(map[string]struct{})
	for _, candidate := range candidates {
		id := candidate.Channel.Id
		if _, exists := channelSet[id]; !exists {
			ids = append(ids, id)
			channelSet[id] = struct{}{}
		}
		domain := routePoolFaultDomain(candidate.Member, candidate.Channel)
		if _, exists := domainSet[domain]; !exists {
			domains = append(domains, domain)
			domainSet[domain] = struct{}{}
		}
	}
	snapshot, err := gatewayruntime.LoadRouteHealthSnapshot(c, ids, domains, model, requestType)
	if err != nil {
		platformobservability.SysError("load route pool advisory health: " + err.Error())
	}
	cache := getRouteHealthRequestCache(c)
	cache.prefetch = &routeHealthPrefetch{model: model, requestType: requestType,
		channels: channelSet, domains: domainSet, snapshot: snapshot}
}

func routePoolCredentialHealth(c *gin.Context, channelID int) (gatewayruntime.ChannelHealth, bool) {
	if cache := getRouteHealthRequestCache(c); cache != nil {
		if prefetch := cache.prefetch; prefetch != nil {
			if _, requested := prefetch.channels[channelID]; requested {
				health, found := prefetch.snapshot.Credentials[channelID]
				return health, found
			}
		}
	}
	return gatewayruntime.GetChannelHealth(channelID, "__channel_credentials__")
}

// routePoolChannelHealth overlays user-scoped transient state on shared
// latency/reliability observations for authenticated requests. Shared state
// remains a soft score input, but a user's transient failure cannot cool a
// route for other users.
func routePoolChannelHealth(
	c *gin.Context,
	channelID int,
	modelName string,
	requestType gatewayruntime.RequestType,
) (gatewayruntime.ChannelHealth, bool) {
	cache := getRouteHealthRequestCache(c)
	if cache != nil && cache.prefetch != nil {
		prefetch := cache.prefetch
		if prefetch.model == modelName && prefetch.requestType == requestType {
			if _, requested := prefetch.channels[channelID]; requested {
				health, found := prefetch.snapshot.Channels[channelID]
				return health, found
			}
		}
	}
	cacheKey := strings.Join([]string{"channel", strconv.Itoa(channelID), modelName, string(requestType)}, "\x00")
	if cache != nil {
		if cached, ok := cache.channels[cacheKey]; ok {
			return cached.health, cached.found
		}
	}
	var shared gatewayruntime.ChannelHealth
	var sharedFound bool
	var user gatewayruntime.ChannelHealth
	var userFound bool
	if !gatewayruntime.UsesIsolatedRouteHealth(c) {
		shared, sharedFound = gatewayruntime.GetChannelHealth(channelID, modelName, requestType)
	} else {
		// Both reads are independent Redis lookups. Do them concurrently so a
		// transiently slow Redis server contributes one wait, not two.
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			shared, sharedFound = gatewayruntime.GetChannelHealth(channelID, modelName, requestType)
		}()
		go func() {
			defer wg.Done()
			user, userFound = gatewayruntime.GetUserChannelHealth(c, channelID, modelName, requestType)
		}()
		wg.Wait()
	}
	if !gatewayruntime.UsesIsolatedRouteHealth(c) {
		if cache != nil {
			cache.channels[cacheKey] = routeHealthCacheEntry{health: shared, found: sharedFound}
		}
		return shared, sharedFound
	}
	shared.State = ""
	shared.ConsecutiveRetryableFailures = 0
	shared.RecoveryProbeSuccesses = 0
	shared.RecoveryProbeUntil = time.Time{}
	shared.RecoveryProbeSlots = 0
	shared.CoolingUntil = time.Time{}
	shared.LastFailureAt = time.Time{}
	shared.LastFailureRequestID = ""
	if userFound {
		shared.State = user.State
		shared.ConsecutiveRetryableFailures = user.ConsecutiveRetryableFailures
		shared.RecoveryProbeSuccesses = user.RecoveryProbeSuccesses
		shared.RecoveryProbeUntil = user.RecoveryProbeUntil
		shared.RecoveryProbeSlots = user.RecoveryProbeSlots
		shared.CoolingUntil = user.CoolingUntil
		shared.LastFailureAt = user.LastFailureAt
		shared.LastFailureRequestID = user.LastFailureRequestID
	}
	found := sharedFound || userFound
	if cache != nil {
		cache.channels[cacheKey] = routeHealthCacheEntry{health: shared, found: found}
	}
	return shared, found
}

func routePoolFaultDomainHealth(
	c *gin.Context,
	domain string,
	modelName string,
	requestType gatewayruntime.RequestType,
) (gatewayruntime.ChannelHealth, bool) {
	cache := getRouteHealthRequestCache(c)
	if cache != nil && cache.prefetch != nil {
		prefetch := cache.prefetch
		if prefetch.model == modelName && prefetch.requestType == requestType {
			if _, requested := prefetch.domains[domain]; requested {
				health, found := prefetch.snapshot.Domains[domain]
				return health, found
			}
		}
	}
	cacheKey := strings.Join([]string{"domain", domain, modelName, string(requestType)}, "\x00")
	if cache != nil {
		if cached, ok := cache.domains[cacheKey]; ok {
			return cached.health, cached.found
		}
	}
	var health gatewayruntime.ChannelHealth
	var found bool
	if gatewayruntime.UsesIsolatedRouteHealth(c) {
		health, found = gatewayruntime.GetUserFaultDomainHealth(c, domain, modelName, requestType)
	} else {
		health, found = gatewayruntime.GetFaultDomainHealth(domain, modelName, requestType)
	}
	if cache != nil {
		cache.domains[cacheKey] = routeHealthCacheEntry{health: health, found: found}
	}
	return health, found
}

func activeRoutePoolCircuit(health gatewayruntime.ChannelHealth, found bool, now time.Time) bool {
	if !found {
		return false
	}
	return health.State == gatewayruntime.ChannelHealthCooling && health.CoolingUntil.After(now) ||
		health.State == gatewayruntime.ChannelHealthHalfOpen &&
			(health.CoolingUntil.After(now) || health.RecoveryProbeUntil.After(now))
}
