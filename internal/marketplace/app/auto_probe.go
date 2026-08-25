package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

const (
	autoProbeWorkerLimit = 4
	autoProbeLeaseTTL    = 10 * time.Minute
	autoProbeLeasePrefix = "codego:marketplace:auto-probe:"
)

var autoProbeOnce sync.Once
var autoProbeRunning sync.Map
var autoProbeSlots = make(chan struct{}, autoProbeWorkerLimit)

const autoProbeReleaseScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`

// StartMarketplaceAutoProbeTask starts the owner-configured channel probes.
func StartMarketplaceAutoProbeTask() {
	autoProbeOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			runDueMarketplaceAutoProbes()
			for range ticker.C {
				runDueMarketplaceAutoProbes()
			}
		}()
	})
}

func runDueMarketplaceAutoProbes() {
	if platformdb.DB == nil {
		return
	}
	var channels []marketplaceschema.Channel
	err := platformdb.DB.Where(
		"auto_probe_enabled = ? AND status IN ?",
		true,
		[]string{marketplacedomain.LifecycleActive, marketplacedomain.LifecycleDegraded},
	).Find(&channels).Error
	if err != nil {
		platformobservability.SysError("list marketplace auto probes: " + err.Error())
		return
	}
	now := time.Now().UTC()
	for index := range channels {
		channel := channels[index]
		interval := channel.AutoProbeIntervalMinutes
		if interval < 1 {
			interval = 10
		}
		if channel.AutoProbeLastAt != nil && channel.AutoProbeLastAt.Add(time.Duration(interval)*time.Minute).After(now) {
			continue
		}
		if !marketplaceAutoProbeDue(channel, now) {
			continue
		}
		select {
		case autoProbeSlots <- struct{}{}:
		default:
			continue
		}
		if _, loaded := autoProbeRunning.LoadOrStore(channel.ID, struct{}{}); loaded {
			<-autoProbeSlots
			continue
		}
		go func(item marketplaceschema.Channel) {
			defer func() {
				autoProbeRunning.Delete(item.ID)
				<-autoProbeSlots
			}()
			release, acquired := claimMarketplaceAutoProbeLease(item.ID)
			if !acquired {
				return
			}
			defer release()
			runMarketplaceModelProbe(&item)
		}(channel)
	}
}

// marketplaceAutoProbeDue spreads recurring probes across the interval so a
// large marketplace does not create a synchronized upstream burst.
func marketplaceAutoProbeDue(channel marketplaceschema.Channel, now time.Time) bool {
	if channel.AutoProbeLastAt == nil {
		return true
	}
	interval := channel.AutoProbeIntervalMinutes
	if interval < 1 {
		interval = 10
	}
	return !channel.AutoProbeLastAt.Add(time.Duration(interval)*time.Minute + autoProbeJitter(channel.ID, interval)).After(now)
}

func autoProbeJitter(channelID string, intervalMinutes int) time.Duration {
	maxSeconds := intervalMinutes * 60 / 4
	if maxSeconds < 1 {
		return 0
	}
	if maxSeconds > 30 {
		maxSeconds = 30
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(channelID))
	return time.Duration(hash.Sum32()%uint32(maxSeconds+1)) * time.Second
}

func claimMarketplaceAutoProbeLease(channelID string) (func(), bool) {
	if !platformcache.RedisReady() {
		return func() {}, true
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		token = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	value := hex.EncodeToString(token)
	key := autoProbeLeasePrefix + channelID
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	acquired, err := platformcache.RDB.SetNX(ctx, key, value, autoProbeLeaseTTL).Result()
	if err != nil {
		// Redis is coordination only; a cache failure must not disable probes.
		return func() {}, true
	}
	if !acquired {
		return func() {}, false
	}
	return func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer releaseCancel()
		_ = platformcache.RDB.Eval(releaseCtx, autoProbeReleaseScript, []string{key}, value).Err()
	}, true
}

func runMarketplaceModelProbe(channel *marketplaceschema.Channel) {
	status := marketplacedomain.VerificationFailed
	model := strings.TrimSpace(channel.AutoProbeModel)
	models := decodeModels(channel.DeclaredModels)
	if model == "" && len(models) > 0 {
		model = models[0]
	}
	baseURL, baseErr := platformsecurity.DecryptSecret(channel.BaseURLCiphertext)
	credential, credentialErr := platformsecurity.DecryptSecret(channel.CredentialCiphertext)
	if baseErr == nil && credentialErr == nil && containsFold(models, model) {
		upstream, err := fetchUpstreamModels(channel.ProviderType, baseURL, credential)
		if err == nil {
			probeCtx, cancel := context.WithTimeout(context.Background(), autoProbeLeaseTTL)
			defer cancel()
			results, probeErr := probeDeclaredModels(
				probeCtx,
				channel.ProviderType,
				baseURL,
				credential,
				[]string{model},
				upstream,
				nil,
			)
			if probeErr == nil && len(results) == 1 && results[0].Status == marketplacedomain.ModelVerificationPassed {
				status = marketplacedomain.VerificationPassed
			}
		}
	}
	now := time.Now().UTC()
	if err := platformdb.DB.Model(&marketplaceschema.Channel{}).Where("id = ?", channel.ID).Updates(map[string]any{
		"auto_probe_last_status": status,
		"auto_probe_last_at":     now,
	}).Error; err != nil {
		platformobservability.SysError("update marketplace auto probe: " + err.Error())
	}
}
