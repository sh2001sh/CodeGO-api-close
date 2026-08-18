package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
)

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
		health, found := routePoolChannelHealth(c, candidate.Channel.Id, modelName, requestType)
		if found && health.State == gatewayruntime.ChannelHealthCooling && health.CoolingUntil.After(now) {
			continue
		}
		domain := routePoolFaultDomain(candidate.Member, candidate.Channel)
		domainHealth, domainFound := routePoolFaultDomainHealth(c, domain, modelName, requestType)
		if activeRoutePoolCircuit(domainHealth, domainFound, now) {
			continue
		}
		scored := scoredRoutePoolCandidate{
			channel: candidate.Channel, faultDomain: domain,
			score: effectiveRoutePoolCost(candidate.Member, modelName, health),
			cost:  routePoolModelCost(candidate.Member, modelName),
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
	health, _ := routePoolChannelHealth(c, channelID, modelName, requestType)
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
	health, _ := routePoolChannelHealth(c, channelID, modelName, requestType)
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
