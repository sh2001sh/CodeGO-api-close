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
	routePoolFallbackCostThreshold = 0.12
	routePoolContextKey            = "automatic_route_pool_selection"

	routePoolAffinityContextKey = "automatic_route_pool_affinity"
	routePoolAffinityTTL        = 3 * time.Minute
	routePoolSwitchImprovement  = 0.15
)

type scoredRoutePoolCandidate struct {
	channel      *gatewayschema.Channel
	score        float64
	probe        bool
	cost         float64
	health       gatewayruntime.ChannelHealth
	faultDomain  string
	channelProbe bool
	domainProbe  bool
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
func selectAutomaticPoolChannel(c *gin.Context, group, modelName string, retry int) (*gatewayschema.Channel, bool, error) {
	detail, err := gatewaystore.LoadEnabledRoutePool(group)
	if err != nil || detail == nil {
		return nil, detail != nil, err
	}
	candidates, err := gatewaystore.LoadRoutePoolCandidates(group, modelName, detail)
	if err != nil {
		return nil, true, err
	}
	now := time.Now()
	healthy := make([]scoredRoutePoolCandidate, 0, len(candidates))
	probes := make([]scoredRoutePoolCandidate, 0, len(candidates))
	lastResortProbes := make([]scoredRoutePoolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if channelAlreadyUsed(c, candidate.Channel.Id) {
			continue
		}
		faultDomain := routePoolFaultDomain(candidate.Member, candidate.Channel)
		if gatewayruntime.IsFaultDomainExcluded(c, faultDomain) {
			gatewayruntime.ExcludeRouteDecisionCandidate(c, "failed_fault_domain")
			continue
		}
		health, found := gatewayruntime.GetChannelHealth(candidate.Channel.Id, modelName)
		domainHealth, domainFound := gatewayruntime.GetFaultDomainHealth(faultDomain, modelName)
		channelCooling := found && ((health.State == gatewayruntime.ChannelHealthCooling && health.CoolingUntil.After(now)) ||
			(health.State == gatewayruntime.ChannelHealthHalfOpen && (health.CoolingUntil.After(now) || health.RecoveryProbeUntil.After(now))))
		domainCooling := domainFound && ((domainHealth.State == gatewayruntime.ChannelHealthCooling && domainHealth.CoolingUntil.After(now)) ||
			(domainHealth.State == gatewayruntime.ChannelHealthHalfOpen && (domainHealth.CoolingUntil.After(now) || domainHealth.RecoveryProbeUntil.After(now))))
		cost := routePoolModelCost(candidate.Member, modelName)
		if channelCooling || domainCooling {
			lastResortProbes = append(lastResortProbes, scoredRoutePoolCandidate{
				channel: candidate.Channel, score: effectiveRoutePoolCost(candidate.Member, modelName, health), cost: cost,
				health:      health,
				faultDomain: faultDomain, channelProbe: channelCooling, domainProbe: domainCooling,
			})
			continue
		}
		if found && (health.State == gatewayruntime.ChannelHealthCooling || health.State == gatewayruntime.ChannelHealthHalfOpen) {
			probes = append(probes, scoredRoutePoolCandidate{channel: candidate.Channel, score: effectiveRoutePoolCost(candidate.Member, modelName, health), probe: true, cost: cost, faultDomain: faultDomain, channelProbe: true, domainProbe: domainFound && (domainHealth.State == gatewayruntime.ChannelHealthCooling || domainHealth.State == gatewayruntime.ChannelHealthHalfOpen)})
			continue
		}
		scored := scoredRoutePoolCandidate{channel: candidate.Channel, score: effectiveRoutePoolCost(candidate.Member, modelName, health), cost: cost, faultDomain: faultDomain}
		if domainFound && (domainHealth.State == gatewayruntime.ChannelHealthCooling || domainHealth.State == gatewayruntime.ChannelHealthHalfOpen) {
			scored.probe = true
			scored.domainProbe = true
			probes = append(probes, scored)
			continue
		}
		healthy = append(healthy, scored)
	}

	applyRoutePoolTTFTPenalty(healthy, modelName)
	applyRoutePoolTTFTPenalty(probes, modelName)
	applyRoutePoolTTFTPenalty(lastResortProbes, modelName)
	primaryHealthy := routePoolPrimaryCostCandidates(healthy)
	primaryProbes := routePoolPrimaryCostCandidates(probes)
	if len(primaryHealthy) > 0 {
		healthy = primaryHealthy
		probes = primaryProbes
	} else if len(primaryProbes) > 0 {
		probes = primaryProbes
	}
	prepareRoutePoolAffinity(c, detail.Pool.ID, group, modelName)
	if probe := reserveRoutePoolRecoveryProbe(probes, modelName); probe != nil {
		gatewayruntime.SetRouteDecisionProbeMode(c, gatewayruntime.RouteDecisionProbeNormal)
		return selectRoutePoolCandidate(c, detail.Pool.ID, probe), true, nil
	}
	if len(healthy) == 0 {
		if retry > 0 {
			if probe := reserveRoutePoolEmergencyRetryProbe(lastResortProbes, modelName); probe != nil {
				probeMode := gatewayruntime.RouteDecisionProbeEmergency
				if c != nil && c.GetBool(string(constant.ContextKeyRateLimitRetry)) {
					probeMode = gatewayruntime.RouteDecisionProbeRateLimit
				}
				gatewayruntime.SetRouteDecisionProbeMode(c, probeMode)
				return selectRoutePoolCandidate(c, detail.Pool.ID, probe), true, nil
			}
		}
		if probe := reserveRoutePoolLastResortProbe(lastResortProbes, modelName); probe != nil {
			gatewayruntime.SetRouteDecisionProbeMode(c, gatewayruntime.RouteDecisionProbeLastResort)
			return selectRoutePoolCandidate(c, detail.Pool.ID, probe), true, nil
		}
		return nil, true, nil
	}
	if sticky := getRoutePoolStickyCandidate(c, healthy, modelName); sticky != nil {
		return selectRoutePoolCandidate(c, detail.Pool.ID, sticky), true, nil
	}
	return selectRoutePoolCandidate(c, detail.Pool.ID, chooseRoutePoolHealthyCandidate(healthy)), true, nil
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
func ShouldMigrateAutomaticPoolAffinity(group, modelName string, channelID int) bool {
	detail, err := gatewaystore.LoadEnabledRoutePool(group)
	if err != nil || detail == nil || channelID <= 0 {
		return false
	}
	candidates, err := gatewaystore.LoadRoutePoolCandidates(group, modelName, detail)
	if err != nil {
		return false
	}
	now := time.Now()
	healthy := make([]scoredRoutePoolCandidate, 0, len(candidates))
	var current *scoredRoutePoolCandidate
	for _, candidate := range candidates {
		health, found := gatewayruntime.GetChannelHealth(candidate.Channel.Id, modelName)
		if found && health.State == gatewayruntime.ChannelHealthCooling && health.CoolingUntil.After(now) {
			continue
		}
		scored := scoredRoutePoolCandidate{
			channel: candidate.Channel,
			score:   effectiveRoutePoolCost(candidate.Member, modelName, health),
			cost:    routePoolModelCost(candidate.Member, modelName),
		}
		healthy = append(healthy, scored)
		if candidate.Channel.Id == channelID {
			current = &healthy[len(healthy)-1]
		}
	}
	if current == nil || len(healthy) < 2 {
		return false
	}
	applyRoutePoolTTFTPenalty(healthy, modelName)
	for index := range healthy {
		if healthy[index].channel.Id == channelID {
			current = &healthy[index]
			break
		}
	}
	best := chooseLowestRoutePoolCandidate(healthy)
	if best == nil || best.channel.Id == channelID {
		return false
	}
	health, _ := gatewayruntime.GetChannelHealth(channelID, modelName)
	if routePoolHardMigrationRequired(health, routePoolMedianTTFT(healthy, modelName)) {
		return true
	}
	return (health.State == gatewayruntime.ChannelHealthDegraded || routePoolReliabilityNeedsMigration(health)) &&
		best.score <= current.score*(1-routePoolSwitchImprovement)
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

// routePoolPrimaryCostCandidates keeps costly routes as an explicit fallback.
// They are eligible only after no affordable healthy route remains.
func routePoolPrimaryCostCandidates(candidates []scoredRoutePoolCandidate) []scoredRoutePoolCandidate {
	primary := make([]scoredRoutePoolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.cost <= routePoolFallbackCostThreshold {
			primary = append(primary, candidate)
		}
	}
	return primary
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
	health, _ := gatewayruntime.GetChannelHealth(channelID, modelName)
	if routePoolHardMigrationRequired(health, routePoolMedianTTFT(candidates, modelName)) {
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
