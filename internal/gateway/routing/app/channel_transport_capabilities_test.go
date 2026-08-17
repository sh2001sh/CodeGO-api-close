package app

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	gatewaycapability "github.com/sh2001sh/new-api/internal/gateway/capability"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCreateChannelsForInsertMarksCapabilitiesPending(t *testing.T) {
	channels := createChannelsForInsert(AddChannelRequest{
		Channel: &gatewayschema.Channel{Key: "key", Models: "gpt-5"},
	}, []string{"key"})
	require.Len(t, channels, 1)
	require.Equal(t, gatewayschema.CapabilityStatusPending, channels[0].ChannelInfo.ResponsesCapabilities.WebSocket.Status)
	require.Equal(t, "gpt-5", channels[0].ChannelInfo.ResponsesCapabilities.WebSocket.Model)
}

func TestChannelProbeCandidatesSkipDisabledKeysAndPreferTestModel(t *testing.T) {
	preferred := "gpt-5.2"
	channel := &gatewayschema.Channel{
		Key: "disabled-key\nenabled-key", Models: "gpt-5,gpt-5.2", TestModel: &preferred,
		ChannelInfo: gatewayschema.ChannelInfo{
			IsMultiKey: true, MultiKeySize: 2,
			MultiKeyStatusList: map[int]int{0: constant.ChannelStatusManuallyDisabled},
		},
	}
	baseURL := "https://api.example.com"
	channel.BaseURL = &baseURL

	candidates := channelProbeCandidates(channel)
	require.Len(t, candidates, 2)
	require.Equal(t, "enabled-key", candidates[0].APIKey)
	require.Equal(t, 1, candidates[0].KeyIndex)
	require.Equal(t, "gpt-5.2", candidates[0].Model)
	require.Equal(t, "gpt-5", candidates[1].Model)
}

func TestProbeAndPersistChannelCapabilities(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })
	require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}))

	baseURL := "https://api.example.com"
	channel := gatewayschema.Channel{Type: constant.ChannelTypeOpenAI, Key: "key", Models: "gpt-5", BaseURL: &baseURL}
	require.NoError(t, db.Create(&channel).Error)

	originalProbe := probeChannelCandidates
	probeChannelCandidates = func(_ context.Context, candidates []gatewaycapability.ProbeInput) gatewaycapability.ProbeResult {
		require.Len(t, candidates, 1)
		state := gatewayschema.CapabilityProbeState{Status: gatewayschema.CapabilityStatusSupported, Model: candidates[0].Model}
		return gatewaycapability.ProbeResult{WebSocket: state, NativeBackground: state}
	}
	t.Cleanup(func() { probeChannelCandidates = originalProbe })

	require.NoError(t, probeAndPersistChannelCapabilities(context.Background(), channel.Id))
	require.NoError(t, db.First(&channel, channel.Id).Error)
	require.True(t, channel.ChannelInfo.ResponsesCapabilities.SupportsWebSocket())
	require.True(t, channel.ChannelInfo.ResponsesCapabilities.SupportsNativeBackground())
}
