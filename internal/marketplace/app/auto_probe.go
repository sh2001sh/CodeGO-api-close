package app

import (
	"strings"
	"sync"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

var autoProbeOnce sync.Once
var autoProbeRunning sync.Map

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
		if _, loaded := autoProbeRunning.LoadOrStore(channel.ID, struct{}{}); loaded {
			continue
		}
		go func(item marketplaceschema.Channel) {
			defer autoProbeRunning.Delete(item.ID)
			runMarketplaceModelProbe(&item)
		}(channel)
	}
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
			results, probeErr := probeDeclaredModels(
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
