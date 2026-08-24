package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceCodexUsesBearerCompatibleChannel(t *testing.T) {
	require.Equal(t, constant.ChannelTypeOpenAI, providerChannelType("codex"))
}

func TestListOwnerChannelsIncludesIncomeSummary(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.Settlement{},
	))

	channel := marketplaceschema.Channel{
		ID: "channel-income", OwnerUserID: 42, ProviderType: "openai_compatible",
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		Status: marketplacedomain.LifecycleActive,
	}
	group := marketplaceschema.Group{
		ID: "group-income", ChannelID: channel.ID, OwnerUserID: channel.OwnerUserID,
		PublicSlug: "income-group", SystemDisplayName: "收入测试渠道",
		InternalGroupName: "market_income_test", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus:    marketplacedomain.LifecycleActive,
		VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility:         marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)
	now := time.Now().UTC()
	require.NoError(t, db.Create([]marketplaceschema.Settlement{
		{
			ID: "settlement-pending", RequestID: "request-pending", GroupID: group.ID,
			OwnerUserID: 42, ConsumerUserID: 7, ConsumerAmount: 1000,
			PlatformCommission: 50, OwnerNetAmount: 950, Multiplier: 1,
			Status: "pending", AvailableAt: now.Add(time.Hour),
		},
		{
			ID: "settlement-released", RequestID: "request-released", GroupID: group.ID,
			OwnerUserID: 42, ConsumerUserID: 8, ConsumerAmount: 2000,
			PlatformCommission: 100, OwnerNetAmount: 1900, Multiplier: 1,
			Status: "released", AvailableAt: now, ReleasedAt: &now,
		},
	}).Error)

	channels, err := ListOwnerChannels(42)
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.EqualValues(t, 2, channels[0].RequestCount)
	require.EqualValues(t, 2850, channels[0].TotalIncome)
	require.EqualValues(t, 950, channels[0].PendingIncome)
	require.EqualValues(t, 1900, channels[0].ReleasedIncome)
}
