package app

import (
	"context"
	"testing"

	gatewaycapability "github.com/sh2001sh/new-api/internal/gateway/capability"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceProbePersistsAndSyncsInternalCapabilities(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &gatewayschema.Channel{}))

	internal := gatewayschema.Channel{Key: "internal-key", Models: "gpt-5"}
	require.NoError(t, db.Create(&internal).Error)
	baseURL, err := platformsecurity.EncryptSecret("https://api.example.com")
	require.NoError(t, err)
	credential, err := platformsecurity.EncryptSecret("marketplace-key")
	require.NoError(t, err)
	channel := marketplaceschema.Channel{
		ID: "transport-channel", OwnerUserID: 1, ProviderType: "openai_compatible",
		BaseURLCiphertext: baseURL, CredentialCiphertext: credential,
		DeclaredModels: `["gpt-5"]`, Status: "draft", InternalChannelID: &internal.Id,
	}
	require.NoError(t, db.Create(&channel).Error)

	originalProbe := probeMarketplaceCandidates
	probeMarketplaceCandidates = func(_ context.Context, candidates []gatewaycapability.ProbeInput) gatewaycapability.ProbeResult {
		require.Len(t, candidates, 1)
		require.Equal(t, "marketplace-key", candidates[0].APIKey)
		state := gatewayschema.CapabilityProbeState{Status: gatewayschema.CapabilityStatusSupported, Model: candidates[0].Model}
		return gatewaycapability.ProbeResult{WebSocket: state, NativeBackground: state}
	}
	t.Cleanup(func() { probeMarketplaceCandidates = originalProbe })

	require.NoError(t, probeAndPersistMarketplaceCapabilities(context.Background(), channel.ID))
	require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
	var persisted gatewayschema.ResponsesCapabilities
	require.NoError(t, platformencoding.Unmarshal([]byte(channel.TransportCapabilities), &persisted))
	require.True(t, persisted.SupportsWebSocket())

	require.NoError(t, db.First(&internal, internal.Id).Error)
	require.True(t, internal.ChannelInfo.ResponsesCapabilities.SupportsWebSocket())
}

func TestMarketplaceNewChannelCapabilitiesStartPending(t *testing.T) {
	channel := &marketplaceschema.Channel{DeclaredModels: `["gpt-5"]`}
	markMarketplaceCapabilitiesPending(channel)
	capabilities := decodeMarketplaceCapabilities(channel.TransportCapabilities)
	require.Equal(t, gatewayschema.CapabilityStatusPending, capabilities.WebSocket.Status)
	require.Equal(t, "gpt-5", capabilities.WebSocket.Model)
}

func TestCreateMarketplaceChannelQueuesTransportCapabilityDetection(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.ChannelIDSequence{},
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
	))

	var queuedChannelID string
	originalCapabilityQueue := queueMarketplaceCapabilityProbe
	originalVerificationQueue := queueMarketplaceVerification
	queueMarketplaceCapabilityProbe = func(channelID string) { queuedChannelID = channelID }
	queueMarketplaceVerification = func(string) error { return nil }
	t.Cleanup(func() {
		queueMarketplaceCapabilityProbe = originalCapabilityQueue
		queueMarketplaceVerification = originalVerificationQueue
	})

	view, err := CreateMarketplaceChannel(42, CreateChannelRequest{
		ProviderType:   "openai_compatible",
		SourceLabel:    "Codex Plus",
		BaseURL:        "https://8.8.8.8",
		APIKey:         "marketplace-key",
		DeclaredModels: []string{"gpt-5"},
		Multiplier:     1,
		Visibility:     marketplacedomain.VisibilityPrivate,
		QPS:            1,
	})
	require.NoError(t, err)
	require.Equal(t, view.ID, queuedChannelID)

	var persisted marketplaceschema.Channel
	require.NoError(t, db.First(&persisted, "id = ?", view.ID).Error)
	capabilities := decodeMarketplaceCapabilities(persisted.TransportCapabilities)
	require.Equal(t, gatewayschema.CapabilityStatusPending, capabilities.WebSocket.Status)
	require.Equal(t, "gpt-5", capabilities.WebSocket.Model)
}

func TestUpdateMarketplaceTransportInputsQueuesCapabilityRedetection(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))

	baseURL, err := platformsecurity.EncryptSecret("https://api.example.com")
	require.NoError(t, err)
	credential, err := platformsecurity.EncryptSecret("marketplace-key")
	require.NoError(t, err)
	channel := marketplaceschema.Channel{
		ID: "transport-update", OwnerUserID: 42, ProviderType: "openai_compatible",
		SubmittedSourceLabel: "OpenAI", ApprovedSourceLabel: "OpenAI",
		SourceLabelStatus: marketplacedomain.SourceLabelApproved,
		BaseURLCiphertext: baseURL, CredentialCiphertext: credential,
		DeclaredModels: `["gpt-5"]`, ModelPrices: `{}`, MaxConcurrency: 1, QPS: 1,
		Status: marketplacedomain.LifecycleActive,
	}
	group := marketplaceschema.Group{
		ID: "transport-update-group", ChannelID: channel.ID, OwnerUserID: channel.OwnerUserID,
		PublicSlug: "mg_transport_update", SystemDisplayName: "OpenAI 1.00x",
		InternalGroupName: "OpenAI-transport", OwnerDisplayName: "owner",
		SourceType:       marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicySubscriptionAndUniversal,
		Multiplier:       1, RoutingVersion: 1, LifecycleStatus: marketplacedomain.LifecycleActive,
		VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility:         marketplacedomain.VisibilityPrivate,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	var queuedChannelID string
	originalQueue := queueMarketplaceCapabilityProbe
	queueMarketplaceCapabilityProbe = func(channelID string) { queuedChannelID = channelID }
	t.Cleanup(func() { queueMarketplaceCapabilityProbe = originalQueue })

	models := []string{"gpt-5.1"}
	_, err = UpdateOwnerChannel(channel.OwnerUserID, channel.ID, UpdateChannelRequest{DeclaredModels: &models})
	require.NoError(t, err)
	require.Equal(t, channel.ID, queuedChannelID)

	require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
	capabilities := decodeMarketplaceCapabilities(channel.TransportCapabilities)
	require.Equal(t, gatewayschema.CapabilityStatusPending, capabilities.WebSocket.Status)
	require.Equal(t, "gpt-5.1", capabilities.WebSocket.Model)
}
