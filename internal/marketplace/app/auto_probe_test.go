package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceBackgroundProbeYieldsToLiveTraffic(t *testing.T) {
	channelID := 10991
	channel := marketplaceschema.Channel{ID: "busy-probe", InternalChannelID: &channelID, Status: marketplacedomain.LifecycleActive, AutoProbeEnabled: true}
	release := gatewayruntime.BeginChannelRequest(channelID)
	defer release()
	_, _, started := beginMarketplaceBackgroundProbe(context.Background(), &channel, "auto")
	require.False(t, started, "a background probe must yield to real requests")
}

func TestMarketplaceAutoProbeStopsInFlightRequestsWhenDisabled(t *testing.T) {
	for _, stage := range []string{"models", "inference"} {
		t.Run(stage, func(t *testing.T) {
			db := openMarketplaceAppTestDB(t)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))
			started, stopServer := make(chan struct{}), make(chan struct{})
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if stage == "inference" && r.Method == http.MethodGet {
					fmt.Fprint(w, `{"data":[{"id":"gpt-5"}]}`)
					return
				}
				close(started)
				select {
				case <-r.Context().Done():
				case <-stopServer:
				}
			}))
			t.Cleanup(func() { close(stopServer); server.Close() })
			baseURL, err := platformsecurity.EncryptSecret(server.URL)
			require.NoError(t, err)
			credential, err := platformsecurity.EncryptSecret("probe-test-key")
			require.NoError(t, err)
			channel := marketplaceschema.Channel{
				ID: "cancel-" + stage, ProviderType: "openai_compatible", Status: marketplacedomain.LifecycleActive,
				AutoProbeEnabled: true, AutoProbeModel: "gpt-5", DeclaredModels: `["gpt-5"]`,
				BaseURLCiphertext: baseURL, CredentialCiphertext: credential,
			}
			require.NoError(t, db.Create(&channel).Error)
			done := make(chan struct{})
			go func() { runMarketplaceModelProbe(&channel); close(done) }()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("probe did not start")
			}
			// Simulate disabling from a different control replica: no local
			// registry cancellation is used, so the database watcher must stop it.
			require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", channel.ID).Update("auto_probe_enabled", false).Error)
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("disabled probe kept its upstream request running")
			}
			require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
			require.Nil(t, channel.AutoProbeLastAt, "cancellation must not be saved as a channel failure")
			if stage == "models" {
				require.EqualValues(t, 1, requests.Load(), "must not start inference after cancellation")
			} else {
				require.EqualValues(t, 2, requests.Load())
			}
		})
	}
}

func TestMarketplaceAutoProbeDueUsesStableJitter(t *testing.T) {
	last := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	channel := marketplaceschema.Channel{
		ID:                       "channel-a",
		AutoProbeIntervalMinutes: 10,
		AutoProbeLastAt:          &last,
	}
	jitter := autoProbeJitter(channel.ID, channel.AutoProbeIntervalMinutes)
	if jitter < 0 || jitter > 30*time.Second {
		t.Fatalf("jitter = %v, want [0, 30s]", jitter)
	}
	if got := autoProbeJitter(channel.ID, channel.AutoProbeIntervalMinutes); got != jitter {
		t.Fatalf("jitter is not stable: first=%v second=%v", jitter, got)
	}
	if marketplaceAutoProbeDue(channel, last.Add(10*time.Minute+jitter-time.Nanosecond)) {
		t.Fatal("probe became due before interval plus jitter")
	}
	if !marketplaceAutoProbeDue(channel, last.Add(10*time.Minute+jitter)) {
		t.Fatal("probe did not become due at interval plus jitter")
	}
}

func TestMarketplaceAutoProbeDueRunsInitialProbeImmediately(t *testing.T) {
	channel := marketplaceschema.Channel{ID: "initial", AutoProbeIntervalMinutes: 10}
	if !marketplaceAutoProbeDue(channel, time.Now().UTC()) {
		t.Fatal("initial probe should be due")
	}
}

func TestMarketplaceAutoProbeRechecksSuspendedChannelBeforeRequest(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}))
	channel := marketplaceschema.Channel{
		ID: "suspended-auto-probe", Status: marketplacedomain.LifecycleSuspended,
		AutoProbeEnabled: true, AutoProbeModel: "gpt-5", DeclaredModels: `["gpt-5"]`,
	}
	require.NoError(t, db.Create(&channel).Error)

	staleActive := channel
	staleActive.Status = marketplacedomain.LifecycleActive
	runMarketplaceModelProbe(&staleActive)

	require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
	require.Nil(t, channel.AutoProbeLastAt)
}
