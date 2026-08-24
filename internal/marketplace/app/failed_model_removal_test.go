package app

import (
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestOwnerCanRemoveOnlyFailedModelAndRetainEvidence(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.Group{},
		&marketplaceschema.VerificationRun{}, &marketplaceschema.GPT56MappingRun{},
	))
	channel := marketplaceschema.Channel{
		ID: "remove-failed-model", OwnerUserID: 42, ProviderType: "openai_compatible",
		DeclaredModels:           `["gpt-5.6-sol","broken-model"]`,
		ModelPrices:              `{"gpt-5.6-sol":{"input_price_per_million":1,"output_price_per_million":2},"broken-model":{"input_price_per_million":3,"output_price_per_million":4}}`,
		ModelVerificationResults: `[{"model":"gpt-5.6-sol","status":"passed","listed":true},{"model":"broken-model","status":"failed","listed":true,"error":"timeout"}]`,
		ConnectivityTestStatus:   marketplacedomain.VerificationFailed,
		AutoProbeEnabled:         true, AutoProbeIntervalMinutes: 10, AutoProbeModel: "broken-model",
		Status: marketplacedomain.LifecycleDraft,
	}
	group := marketplaceschema.Group{
		ID: "remove-failed-group", ChannelID: channel.ID, OwnerUserID: channel.OwnerUserID,
		PublicSlug: "remove-failed", SystemDisplayName: "测试渠道",
		InternalGroupName: "market_remove_failed", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus:    marketplacedomain.LifecycleDraft,
		VerificationStatus: marketplacedomain.VerificationFailed,
		Visibility:         marketplacedomain.VisibilityPrivate,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	view, err := RemoveOwnerFailedChannelModel(channel.OwnerUserID, channel.ID, "broken-model")
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.6-sol"}, view.DeclaredModels)
	require.Len(t, view.ModelVerificationResults, 1)
	require.Equal(t, "gpt-5.6-sol", view.AutoProbeModel)
	require.NotContains(t, view.ModelPrices, "broken-model")
	require.Equal(t, marketplacedomain.VerificationPassed, view.ConnectivityTestStatus)
	require.Equal(t, marketplacedomain.VerificationQueued, view.VerificationStatus)
}

func TestFailedModelRemovalRejectsPassedAndLastModel(t *testing.T) {
	channel := &marketplaceschema.Channel{
		DeclaredModels:           `["passed-model","failed-model"]`,
		ModelVerificationResults: `[{"model":"passed-model","status":"passed","listed":true},{"model":"failed-model","status":"failed","listed":true}]`,
	}
	group := &marketplaceschema.Group{}
	_, err := removeFailedChannelModel(channel, group, "passed-model")
	require.EqualError(t, err, "只能剔除最近一次检测中失败或上游未列出的模型")

	channel.DeclaredModels = `["failed-model"]`
	_, err = removeFailedChannelModel(channel, group, "failed-model")
	require.EqualError(t, err, "渠道至少需要保留一个模型")
}
