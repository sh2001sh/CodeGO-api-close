package app

import (
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestChannelOwnerCanToggleSecurityAlertEmailWithoutReverification(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	channel := marketplaceschema.Channel{
		ID: "security-alert-owner", OwnerUserID: 42, ProviderType: "openai_compatible",
		Status: marketplacedomain.LifecycleActive, ConnectivityTestStatus: marketplacedomain.VerificationPassed,
	}
	group := autoRouteTestGroup("security-alert-owner-group", channel.ID, channel.OwnerUserID, 1)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	for _, enabled := range []bool{true, false} {
		view, err := UpdateOwnerChannel(42, channel.ID, UpdateChannelRequest{SecurityAlertEmailEnabled: &enabled})
		require.NoError(t, err)
		require.Equal(t, enabled, view.SecurityAlertEmailEnabled)
		require.Equal(t, marketplacedomain.LifecycleActive, view.LifecycleStatus)
	}
	var saved marketplaceschema.Channel
	require.NoError(t, db.First(&saved, "id = ?", channel.ID).Error)
	require.False(t, saved.SecurityAlertEmailEnabled)

	enabled := true
	_, err := UpdateOwnerChannel(43, channel.ID, UpdateChannelRequest{SecurityAlertEmailEnabled: &enabled})
	require.Error(t, err)
	require.NoError(t, db.First(&saved, "id = ?", channel.ID).Error)
	require.False(t, saved.SecurityAlertEmailEnabled)
}
