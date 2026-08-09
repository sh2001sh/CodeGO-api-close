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

	routePoolAffinityContextKey = "automatic_route_pool_affinity"
	routePoolAffinityTTL        = 3 * time.Minute
	routePoolSwitchImprovement  = 0.15
	routePoolLatencyCostPremium = 0.15
)

type scoredRoutePoolCandidate struct {
	channel         *gatewayschema.Channel
	score           float64
	probe           bool
	cost            float64
	health          gatewayruntime.ChannelHealth
	faultDomain     string
	channelProbe    bool
	credentialProbe bool
	domainProbe     bool
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
	candidates, err := gatewaystore.LoadRoutePoolCandidates(group, modelName, detail)
	if err != nil {
		return nil, true, err
	}
	now := time.Now()
	requestType := gatewayruntime.RequestTypeFromContext(c)
	healthy, probes, lastResortProbes := buildRoutePoolCandidateSets(c, candidates, modelName, requestType, now)

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
	if !allowLastResort {
		return nil, true, nil
	}
	if probe := reserveRoutePoolRecoveryProbe(probes, modelName, requestType); probe != nil {
		gatewayruntime.SetRouteDecisionProbeMode(c, gatewayruntime.RouteDecisionProbeNormal)
		return selectRoutePoolCandidate(c, detail.Pool.ID, probe), true, nil
	}
	if len(healthy) == 0 {
		if retry > 0 {
			if probe := reserveRoutePoolEmergencyRetryProbe(lastResortProbes, modelName, requestType); probe != nil {
				probeMode := gatewayruntime.RouteDecisionProbeEmergency
				if c != nil && c.GetBool(string(constant.ContextKeyRateLimitRetry)) {
					probeMode = gatewayruntime.RouteDecisionProbeRateLimit
				}
				gatewayruntime.SetRouteDecisionProbeMode(c, probeMode)
				return selectRoutePoolCandidate(c, detail.Pool.ID, probe), true, nil
			}
		}
		if probe := reserveRoutePoolLastResortProbe(lastResortProbes, modelName, requestType); probe != nil {
			gatewayruntime.SetRouteDecisionProbeMode(c, gatewayruntime.RouteDecisionProbeLastResort)
			return selectRoutePoolCandidate(c, detail.Pool.ID, probe), true, nil
		}
		// The probe lease is a concurrency guard, not an availability gate. If
		// another request owns it, keep the model usable with the best known
		// cooling route rather than returning an avoidable 503.
		if fallback := chooseBestRoutePoolLastResortProbe(lastResortProbes); fallback != nil {
			if !gatewayruntime.AcquireAllCoolingFallback(c, group, modelName, requestType) {
				return nil, true, nil
			}
			gatewayruntime.SetRouteDecisionProbeMode(c, gatewayruntime.RouteDecisionProbeLastResort)
			return selectRoutePoolCandidate(c, detail.Pool.ID, fallback), true, nil
		}
		return nil, true, nil
	}
	return nil, true, nil
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

// RecordAutomaticPoolAffinity keeps an unbound token on the selected pool
// member for a short period. Explicit cache affinity remains independent.
func RecordAutomaticPoolAffinity(c *gin.Context, selectedChannelID int) {
	if c == nil {
		return
	}
	affinity, ok := c.Get(routePoolAffinityContextKey)
	if !ok {
		return
	}
	value, ok := affinity.(routePoolAffinity)
	if !ok || value.CacheKey == "" {
		return
	}
	if successfulChannelID := c.GetInt(string(constant.ContextKeyChannelId)); successfulChannelID > 0 {
		selectedChannelID = successfulChannelID
	}
	if selectedChannelID > 0 {
		_ = gatewayruntime.RecordPreferredChannel(value.CacheKey, selectedChannelID, int(routePoolAffinityTTL.Seconds()))
	}
}

// ShouldMigrateAutomaticPoolAffinity permits explicit cache affinity to escape
// an unhealthy automatic-pool member without making healthy sessions drift.
func ShouldMigrateAutomaticPoolAffinity(c *gin.Context, group, modelName string, channelID int) bool {
	detail, err := gatewaystore.LoadEnabledRoutePool(group)
	if err != nil || detail == nil || channelID <= 0 {
		return false
	}
	candidates, err := gatewaystore.LoadRoutePoolCandidates(group, modelName, detail)
	if err != nil {
		return false
	}
	now := time.Now()
	requestType := gatewayruntime.RequestTypeFromContext(c)
	healthy := make([]scoredRoutePoolCandidate, 0, len(candidates))
	var current *scoredRoutePoolCandidate
	for _, candidate := range candidates {
		health, found := gatewayruntime.GetChannelHealth(candidate.Channel.Id, modelName, requestType)
		if found && health.State == gatewayruntime.ChannelHealthCooling && health.CoolingUntil.After(now) {
			continue
		}
		scored := scoredRoutePoolCandidate{
			channel:     candidate.Channel,
			faultDomain: routePoolFaultDomain(candidate.Member, candidate.Channel),
			score:       effectiveRoutePoolCost(candidate.Member, modelName, health),
			cost:        routePoolModelCost(candidate.Member, modelName),
		}
		healthy = append(healthy, scored)
		if candidate.Channel.Id == channelID {
			current = &healthy[len(healthy)-1]
		}
	}
	if current == nil || len(healthy) < 2 {
		return false
	}
	applyRoutePoolTTFTPenalty(healthy, modelName, requestType)
	for index := range healthy {
		if healthy[index].channel.Id == channelID {
			current = &healthy[index]
			break
		}
	}
	best := chooseDifferentFaultDomainRoutePoolCandidate(healthy, current)
	if best == nil || best.channel.Id == channelID {
		return false
	}
	health, _ := gatewayruntime.GetChannelHealth(channelID, modelName, requestType)
	medianTTFT := routePoolMedianTTFT(healthy, modelName, requestType)
	if routePoolHardMigrationRequired(health, medianTTFT) {
		return true
	}
	if routePoolLatencyMigrationRequired(health, medianTTFT) && best.cost <= current.cost*(1+routePoolLatencyCostPremium) {
		return true
	}
	return (health.State == gatewayruntime.ChannelHealthDegraded || routePoolReliabilityNeedsMigration(health)) &&
		best.score <= current.score*(1-routePoolSwitchImprovement)
}

func chooseDifferentFaultDomainRoutePoolCandidate(candidates []scoredRoutePoolCandidate, current *scoredRoutePoolCandidate) *scoredRoutePoolCandidate {
	if current == nil || current.channel == nil || current.faultDomain == "" {
		return nil
	}
	alternatives := make([]scoredRoutePoolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.channel == nil || candidate.channel.Id == current.channel.Id || candidate.faultDomain == current.faultDomain {
			continue
		}
		alternatives = append(alternatives, candidate)
	}
	return chooseLowestRoutePoolCandidate(routePoolPreferredHealthTier(alternatives))
}

func selectRoutePoolCandidate(c *gin.Context, poolID int64, candidate *scoredRoutePoolCandidate) *gatewayschema.Channel {
	if candidate == nil {
		return nil
	}
	if c != nil {
		gatewayruntime.MarkAutomaticPool(c)
		c.Set(routePoolContextKey, RoutePoolSelection{PoolID: poolID, ProcurementCostMultiplier: candidate.cost})
		if candidate.faultDomain != "" {
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

func prepareRoutePoolAffinity(c *gin.Context, poolID int64, group, modelName string) {
	if c == nil || poolID <= 0 || c.GetInt(string(constant.ContextKeyTokenId)) <= 0 {
		return
	}
	key := strings.Join([]string{
		"route_pool",
		strconv.FormatInt(poolID, 10),
		strconv.Itoa(c.GetInt(string(constant.ContextKeyTokenId))),
		group,
		modelName,
	}, ":")
	c.Set(routePoolAffinityContextKey, routePoolAffinity{CacheKey: key})
}

func getRoutePoolStickyCandidate(c *gin.Context, candidates []scoredRoutePoolCandidate, modelName string) *scoredRoutePoolCandidate {
	if c == nil {
		return nil
	}
	value, ok := c.Get(routePoolAffinityContextKey)
	if !ok {
		return nil
	}
	affinity, ok := value.(routePoolAffinity)
	if !ok || affinity.CacheKey == "" {
		return nil
	}
	channelID, found, err := gatewayruntime.GetPreferredChannel(affinity.CacheKey)
	if err != nil || !found {
		return nil
	}
	var sticky *scoredRoutePoolCandidate
	for index := range candidates {
		if candidates[index].channel.Id == channelID {
			sticky = &candidates[index]
			break
		}
	}
	if sticky == nil || channelAlreadyUsed(c, channelID) {
		gatewayruntime.InvalidatePreferredChannel(affinity.CacheKey)
		return nil
	}
	requestType := gatewayruntime.RequestTypeFromContext(c)
	health, _ := gatewayruntime.GetChannelHealth(channelID, modelName, requestType)
	if routePoolHardMigrationRequired(health, routePoolMedianTTFT(candidates, modelName, requestType)) {
		gatewayruntime.InvalidatePreferredChannel(affinity.CacheKey)
		return nil
	}
	best := chooseLowestRoutePoolCandidate(candidates)
	if best != nil && best.channel.Id != channelID && best.score <= sticky.score*(1-routePoolSwitchImprovement) {
		return nil
	}
	return sticky
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
