package runtime

import (
	"time"

	"github.com/gin-gonic/gin"
)

type RouteHealthSnapshot struct {
	Channels    map[int]ChannelHealth
	Domains     map[string]ChannelHealth
	Credentials map[int]ChannelHealth
}

// LoadRouteHealthSnapshot bounds advisory reads by one Redis round trip per
// routing round, irrespective of the number of pool members. Admission leases
// remain live reads and are never taken from this snapshot.
func LoadRouteHealthSnapshot(c *gin.Context, channelIDs []int, domains []string, model string, requestType RequestType) (RouteHealthSnapshot, error) {
	type channelKeys struct {
		id                       int
		shared, credential, user string
	}
	type domainKeys struct{ domain, key string }
	keys := make([]string, 0, 3*len(channelIDs)+len(domains))
	seen := make(map[string]bool, cap(keys))
	add := func(key string) {
		if key != "" && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	isolated := UsesIsolatedRouteHealth(c)
	channelLookups := make([]channelKeys, 0, len(channelIDs))
	seenIDs := make(map[int]struct{}, len(channelIDs))
	for _, id := range channelIDs {
		if _, exists := seenIDs[id]; exists {
			continue
		}
		seenIDs[id] = struct{}{}
		lookup := channelKeys{id: id, shared: channelHealthKey(id, model, requestType), credential: channelHealthKey(id, channelCredentialHealthModel)}
		add(lookup.shared)
		add(lookup.credential)
		if isolated {
			lookup.user, _ = userChannelHealthKey(c, id, model, requestType)
			add(lookup.user)
		}
		channelLookups = append(channelLookups, lookup)
	}
	domainLookups := make([]domainKeys, 0)
	seenDomains := make(map[string]struct{})
	for _, domain := range domains {
		if domain == "" {
			continue
		}
		if _, exists := seenDomains[domain]; exists {
			continue
		}
		seenDomains[domain] = struct{}{}
		var key string
		if isolated {
			key, _ = userFaultDomainHealthKey(c, domain, model, requestType)
		} else {
			key = faultDomainHealthKey(domain, model, requestType)
		}
		add(key)
		domainLookups = append(domainLookups, domainKeys{domain: domain, key: key})
	}
	values, err := getChannelHealthCache().GetManyWithTimeout(keys, channelHealthReadTimeout)
	snapshot := RouteHealthSnapshot{
		Channels:    make(map[int]ChannelHealth, len(channelLookups)),
		Domains:     make(map[string]ChannelHealth, len(domainLookups)),
		Credentials: make(map[int]ChannelHealth),
	}
	for _, lookup := range channelLookups {
		shared, found := values[lookup.shared]
		if isolated {
			user, userFound := values[lookup.user]
			userFound = userFound && !userRouteHealthEmpty(user)
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
			found = found || userFound
		}
		if found {
			snapshot.Channels[lookup.id] = shared
		}
		if credential, ok := values[lookup.credential]; ok {
			snapshot.Credentials[lookup.id] = credential
		}
	}
	for _, lookup := range domainLookups {
		if health, ok := values[lookup.key]; ok && (!isolated || !userRouteHealthEmpty(health)) {
			snapshot.Domains[lookup.domain] = health
		}
	}
	return snapshot, err
}
