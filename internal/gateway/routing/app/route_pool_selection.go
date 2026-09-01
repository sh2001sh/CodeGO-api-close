package app

import (
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
)

const (
	routePoolExploreRate           = 0.025
	routePoolProbeRate             = 0.02
	routePoolCostRecoveryProbeRate = 0.12
	routePoolCostRecoveryGap       = 0.33
	routePoolContextKey            = "automatic_route_pool_selection"
	routePoolFaultDomainContextKey = "automatic_route_pool_fault_domain"

	routePoolAffinityContextKey = "automatic_route_pool_affinity"
	routePoolAffinityTTL        = 3 * time.Minute
	routePoolSwitchImprovement  = 0.15
	routePoolLatencyCostPremium = 0.15
	// Unknown compaction capabilities remain usable during backfill, but a
	// confirmed-capable route must win whenever one is available.
	routePoolUnknownCompactionPenalty = 1_000_000
)

type scoredRoutePoolCandidate struct {
	channel                  *gatewayschema.Channel
	score                    float64
	compactionCapabilityRank int
	probe                    bool
	cost                     float64
	health                   gatewayruntime.ChannelHealth
	faultDomain              string
	channelProbe             bool
	credentialProbe          bool
	domainProbe              bool
}

// RoutePoolSelection is request-local and consumed only by settlement code.
type RoutePoolSelection struct {
	PoolID                    int64
	ProcurementCostMultiplier float64
}

type routePoolAffinity struct {
	CacheKey string
}

// selectAutomaticPoolChannel returns managed=true when the group has an enabled
// automatic pool. In that case priority and weight are deliberately ignored.
func selectAutomaticPoolChannel(c *gin.Context, group, modelName string, retry int, allowLastResort bool) (*gatewayschema.Channel, bool, error) {
	detail, err := gatewaystore.LoadEnabledRoutePool(group)
	if err != nil || detail == nil {
		return nil, detail != nil, err
	}
	if scope := strings.TrimSpace(detail.Pool.ModelScope); scope != "" && !strings.EqualFold(scope, strings.TrimSpace(modelName)) {
		return nil, false, nil
	}
	candidates, err := gatewaystore.LoadRoutePoolCandidates(group, modelName, detail)
	if err != nil {
		return nil, true, err
	}
	now := time.Now()
	requestType := gatewayruntime.RequestTypeFromContext(c)
	healthy, probes, lastResortProbes := buildRoutePoolCandidateSets(c, candidates, modelName, requestType, now, detail.Pool)
	healthy, probes, lastResortProbes = preferRemoteCompactionCandidates(healthy, probes, lastResortProbes)

	applyRoutePoolTTFTPenalty(healthy, modelName, requestType)
	applyRoutePoolTTFTPenalty(probes, modelName, requestType)
	applyRoutePoolTTFTPenalty(lastResortProbes, modelName, requestType)
	healthy = routePoolPreferredHealthTier(healthy)
	prepareRoutePoolAffinity(c, detail.Pool.ID, group, modelName)
	// Healthy routes always win. Recovery probes are only used when no stable
	// route is available, so a half-open member cannot displace live capacity.
	if len(healthy) > 0 {
		if sticky := getRoutePoolStickyCandidate(c, healthy, modelName); sticky != nil {
			return selectRoutePoolCandidate(c, detail.Pool.ID, sticky), true, nil
		}
		return selectRoutePoolCandidate(c, detail.Pool.ID, chooseRoutePoolHealthyCandidate(healthy)), true, nil
	}
	return selectAutomaticPoolFallback(c, detail.Pool.ID, group, modelName, retry, allowLastResort, requestType, probes, lastResortProbes), true, nil
}

func selectAutomaticPoolFallback(
	c *gin.Context,
	poolID int64,
	group, modelName string,
	retry int,
	allowLastResort bool,
	requestType gatewayruntime.RequestType,
	probes, lastResortProbes []scoredRoutePoolCandidate,
) *gatewayschema.Channel {
	if !allowLastResort {
		return nil
	}
	if probe := reserveRoutePoolRecoveryProbe(c, probes, modelName, requestType); probe != nil {
		gatewayruntime.SetRouteDecisionProbeMode(c, gatewayruntime.RouteDecisionProbeNormal)
		return selectRoutePoolCandidate(c, poolID, probe)
	}
	if retry > 0 {
		if probe := reserveRoutePoolEmergencyRetryProbe(c, lastResortProbes, modelName, requestType); probe != nil {
			probeMode := gatewayruntime.RouteDecisionProbeEmergency
			if c != nil && c.GetBool(string(constant.ContextKeyRateLimitRetry)) {
				probeMode = gatewayruntime.RouteDecisionProbeRateLimit
			}
			gatewayruntime.SetRouteDecisionProbeMode(c, probeMode)
			return selectRoutePoolCandidate(c, poolID, probe)
		}
	}
	if probe := reserveRoutePoolLastResortProbe(c, lastResortProbes, modelName, requestType); probe != nil {
		gatewayruntime.SetRouteDecisionProbeMode(c, gatewayruntime.RouteDecisionProbeLastResort)
		return selectRoutePoolCandidate(c, poolID, probe)
	}
	// A busy probe lease is a concurrency guard, not an availability gate.
	fallback := chooseBestRoutePoolLastResortProbe(lastResortProbes)
	if fallback == nil {
		return nil
	}
	if c != nil {
		_ = gatewayruntime.AcquireAllCoolingFallback(c, group, modelName, requestType)
		gatewayruntime.SetRouteDecisionProbeMode(c, gatewayruntime.RouteDecisionProbeLastResort)
	}
	return selectRoutePoolCandidate(c, poolID, fallback)
}

func removeRoutePoolCandidate(candidates []scoredRoutePoolCandidate, channelID int) []scoredRoutePoolCandidate {
	filtered := candidates[:0]
	for _, candidate := range candidates {
		if candidate.channel == nil || candidate.channel.Id != channelID {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func routePoolFaultDomain(member gatewayschema.RoutePoolMember, channel *gatewayschema.Channel) string {
	if configured := strings.ToLower(strings.TrimSpace(member.FaultDomain)); configured != "" {
		return configured
	}
	if channel == nil {
		return ""
	}
	return gatewayruntime.ChannelFaultDomain(channel.Type, channel.GetBaseURL())
}

func selectRoutePoolCandidate(c *gin.Context, poolID int64, candidate *scoredRoutePoolCandidate) *gatewayschema.Channel {
	if candidate == nil {
		return nil
	}
	if c != nil {
		gatewayruntime.MarkAutomaticPool(c)
		c.Set(routePoolContextKey, RoutePoolSelection{PoolID: poolID, ProcurementCostMultiplier: candidate.cost})
		if candidate.faultDomain != "" {
			c.Set(routePoolFaultDomainContextKey, candidate.faultDomain)
			c.Set("channel_fault_domain", candidate.faultDomain)
		}
	}
	return candidate.channel
}

// GetRoutePoolSelection returns the selected procurement snapshot for the request.
func GetRoutePoolSelection(c *gin.Context) (RoutePoolSelection, bool) {
	if c == nil {
		return RoutePoolSelection{}, false
	}
	value, ok := c.Get(routePoolContextKey)
	if !ok {
		return RoutePoolSelection{}, false
	}
	selection, ok := value.(RoutePoolSelection)
	return selection, ok && selection.PoolID > 0 && selection.ProcurementCostMultiplier > 0
}

// SetRoutePoolSelectionSnapshot restores a previously selected procurement
// route for deferred execution without running route selection again.
func SetRoutePoolSelectionSnapshot(c *gin.Context, selection RoutePoolSelection, faultDomain string) {
	if c == nil || selection.PoolID <= 0 || selection.ProcurementCostMultiplier <= 0 {
		return
	}
	c.Set(routePoolContextKey, selection)
	if faultDomain != "" {
		c.Set(routePoolFaultDomainContextKey, faultDomain)
		c.Set("channel_fault_domain", faultDomain)
	}
}

func chooseRoutePoolHealthyCandidate(candidates []scoredRoutePoolCandidate) *scoredRoutePoolCandidate {
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score < candidates[j].score })
	best := candidates[0]
	if len(candidates) == 1 || rand.Float64() >= routePoolExploreRate {
		return &best
	}
	limit := best.score * 1.15
	explorable := make([]scoredRoutePoolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.score <= limit {
			explorable = append(explorable, candidate)
		}
	}
	if len(explorable) == 0 {
		return &best
	}
	selected := explorable[rand.Intn(len(explorable))]
	return &selected
}

// routePoolPreferredHealthTier keeps cost inside the selected stability tier.
// A degraded cheap route must not mask a healthy, more expensive alternative.
func routePoolPreferredHealthTier(candidates []scoredRoutePoolCandidate) []scoredRoutePoolCandidate {
	stable := make([]scoredRoutePoolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.health.State != gatewayruntime.ChannelHealthDegraded {
			stable = append(stable, candidate)
		}
	}
	if len(stable) > 0 {
		return stable
	}
	return candidates
}

// routePoolRecoveryProbeRate lets a clearly cheaper model route demonstrate
// recovery sooner, while retaining a stable fallback for the remaining traffic.
func routePoolRecoveryProbeRate(healthy, probes []scoredRoutePoolCandidate) float64 {
	if len(healthy) == 0 || len(probes) == 0 {
		return routePoolProbeRate
	}
	cheapestHealthy := chooseLowestRoutePoolCandidate(healthy)
	cheapestProbe := chooseLowestRoutePoolCandidate(probes)
	if cheapestHealthy == nil || cheapestProbe == nil || cheapestHealthy.cost <= 0 || cheapestProbe.cost <= 0 {
		return routePoolProbeRate
	}
	if cheapestProbe.cost <= cheapestHealthy.cost*(1-routePoolCostRecoveryGap) {
		return routePoolCostRecoveryProbeRate
	}
	return routePoolProbeRate
}

func chooseLowestRoutePoolCandidate(candidates []scoredRoutePoolCandidate) *scoredRoutePoolCandidate {
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score < candidates[j].score })
	return &candidates[0]
}

func channelAlreadyUsed(c *gin.Context, channelID int) bool {
	if c == nil || channelID <= 0 {
		return false
	}
	needle := strconv.Itoa(channelID)
	for _, used := range c.GetStringSlice("use_channel") {
		if strings.TrimSpace(used) == needle {
			return true
		}
	}
	return false
}
