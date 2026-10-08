package routing

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// usable is applied to weighted, affinity and cooldown fallbacks alike.
// A cached route never grants access by itself or makes an expired key usable.
func (b *planBuilder) usable(ch *catalog.Channel, cred catalog.Credential) bool {
	if !officialDomainAllowed(b.officialSelections, b.targets, ch.ID) {
		return false
	}
	if b.officialUsedDomains != nil && (b.hasChannel(ch.ID) || b.officialUsedDomains[b.officialDomain(ch)]) {
		return false
	}
	now := time.Unix(0, b.now)
	if !cred.ExpiresAt.IsZero() && !now.Before(cred.ExpiresAt) {
		return false
	}
	user := b.req.Principal.UserID
	if !b.groupAllowed() {
		return false
	}
	_, marketGroup := b.snap.Market.Groups[b.group]
	_, personalPool := b.snap.Market.Pools[b.group]
	if !marketGroup && !personalPool && !b.cardFallback && b.snap.AccountProfiles[user].RequiresCardChannel(now) && !ch.MultiplierCardUserEnabled {
		return false
	}
	if pool, ok := b.snap.Market.Pools[b.group]; ok {
		if pool.OwnerUserID != user || !b.poolMemberSupports(pool, ch) {
			return false
		}
	}
	if group, ok := b.snap.Market.Groups[b.group]; ok && !group.Allows(user) {
		return false
	}
	policy, ok := b.snap.Market.Channels[ch.ID]
	if ch.Scope != "marketplace" && !ok {
		if pool, pooled := b.snap.Market.Pools[b.group]; pooled {
			group, factor := b.targetPricing(ch)
			if group == b.group || !b.allowsGroup(group) || (pool.MaxMultiplierPPM > 0 && factor > pool.MaxMultiplierPPM) {
				return false
			}
		}
		return true
	}
	if !ok || policy.Blocked[user] || !b.snap.Market.Groups[policy.GroupName].Allows(user) {
		return false
	}
	factor := policy.Factor(user, now)
	if maximum := b.req.Principal.MaxMarketplaceMultiplierPPM; maximum > 0 && factor > maximum {
		return false
	}
	if pool, ok := b.snap.Market.Pools[b.group]; ok && pool.MaxMultiplierPPM > 0 && factor > pool.MaxMultiplierPPM {
		return false
	}
	return factor >= 0
}

// Catalog user pools use a wildcard entry to project their members. That
// entry is not a claim that every member supports every requested model.
func (b *planBuilder) poolMemberSupports(pool catalog.MarketPoolPolicy, ch *catalog.Channel) bool {
	group, _ := b.targetPricing(ch)
	member := false
	for _, m := range pool.Members {
		name := m.CatalogGroupName
		if name == "" && strings.HasPrefix(m.GroupID, "official:") {
			name = strings.TrimPrefix(m.GroupID, "official:")
		}
		if name == group {
			member = true
			break
		}
	}
	if !member {
		return false
	}
	return b.memberGroupSupports(group, ch.ID)
}

func (b *planBuilder) memberGroupSupports(group string, channelID int64) bool {
	routes := b.snap.Routes[group][b.req.Model]
	if len(routes) == 0 {
		routes = b.snap.Routes[group]["*"]
	}
	for _, route := range routes {
		if route.ChannelID == channelID {
			return true
		}
	}
	return false
}

func (b *planBuilder) officialDomain(ch *catalog.Channel) string {
	if selection, ok := b.officialSelections[ch.ID]; ok {
		return selection.FaultDomain
	}
	return officialFaultDomain(catalog.OfficialPoolMember{}, ch)
}

func (b *planBuilder) enableOfficialIsolation() {
	if b.officialUsedDomains != nil {
		return
	}
	b.officialUsedDomains = map[string]bool{}
	for _, target := range b.targets {
		if ch := b.snap.Channels[target.ChannelID]; ch != nil {
			b.officialUsedDomains[officialFaultDomain(catalog.OfficialPoolMember{}, ch)] = true
		}
	}
}

func (b *planBuilder) targetPricing(ch *catalog.Channel) (string, int64) {
	if policy, ok := b.snap.Market.Channels[ch.ID]; ok {
		return policy.GroupName, policy.Factor(b.req.Principal.UserID, time.Unix(0, b.now))
	}
	group := b.group
	if member, ok := b.poolGroups[ch.ID]; ok {
		return member, int64(math.Round(b.snap.Groups[member].Multiplier * 1000000))
	}
	if b.req.Principal.Group == "zero-hour" && ch.MultiplierCardUserEnabled {
		if _, ok := b.snap.AccountProfiles[b.req.Principal.UserID].PackageCard("zero_hour_multiplier", time.Unix(0, b.now)); ok {
			return group, 0
		}
	}
	if pool, ok := b.snap.Market.Pools[group]; ok {
		for _, member := range pool.GroupIDs {
			name := strings.TrimPrefix(member, "official:")
			if name == member {
				continue
			}
			for _, membership := range ch.Groups {
				if membership == name && b.allowsGroup(name) && b.memberGroupSupports(name, ch.ID) {
					return name, int64(math.Round(b.snap.Groups[name].Multiplier * 1000000))
				}
			}
		}
	}
	factor := int64(1000000)
	if g, ok := b.snap.Groups[group]; ok {
		factor = int64(math.Round(g.Multiplier * 1000000))
	}
	return group, factor
}

func requestedGroups(req *gateway.Request, snap *catalog.Snapshot) []string {
	groups := []string{req.Principal.Group}
	if req.Principal.Group == "auto" {
		groups = append([]string(nil), req.Principal.AutoGroups...)
	}
	if req.Principal.CrossGroupRetry {
		additional := req.Principal.AllowedGroups
		if profile, ok := snap.AccountProfiles[req.Principal.UserID]; ok && profile.AllowedGroups != nil {
			additional = profile.AllowedGroups
		}
		additional = append([]string(nil), additional...)
		sort.SliceStable(additional, func(i, j int) bool {
			a, b := snap.Groups[additional[i]].Multiplier, snap.Groups[additional[j]].Multiplier
			if a == b {
				return additional[i] < additional[j]
			}
			return a < b
		})
		for _, group := range append(append([]string(nil), req.Principal.AutoGroups...), additional...) {
			present := false
			for _, existing := range groups {
				present = present || existing == group
			}
			if !present {
				groups = append(groups, group)
			}
		}
	}
	return groups
}

func (b *planBuilder) groupAllowed() bool {
	if b.req.Principal.Group == "zero-hour" {
		_, active := b.snap.AccountProfiles[b.req.Principal.UserID].PackageCard("zero_hour_multiplier", time.Unix(0, b.now))
		return active && b.group == cardRouteGroup(b.snap)
	}
	if b.req.Principal.Group == "monthly-pass" && b.group == cardRouteGroup(b.snap) {
		return true
	}
	return b.allowsGroup(b.group)
}

func (b *planBuilder) allowsGroup(name string) bool {
	allowed := b.req.Principal.AllowedGroups
	if profile, ok := b.snap.AccountProfiles[b.req.Principal.UserID]; ok && profile.AllowedGroups != nil {
		allowed = profile.AllowedGroups
	}
	if allowed == nil {
		return true
	}
	for _, group := range allowed {
		if group == name {
			return true
		}
	}
	return false
}
