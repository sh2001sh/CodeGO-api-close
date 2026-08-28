package app

import (
	"testing"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestStartBatchMarketplaceTestUsesUserBillingAndRecordsTiming(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))

	internalChannelID := 123
	require.NoError(t, db.Create(&marketplaceschema.Channel{
		ID: "batch-channel", OwnerUserID: 7, ProviderType: "openai_compatible",
		DeclaredModels: `["gpt-5.6-sol"]`, InternalChannelID: &internalChannelID,
		BaseURLCiphertext: "", CredentialCiphertext: "",
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{
		ID: "batch-group", ChannelID: "batch-channel", OwnerUserID: 7,
		PublicSlug: "batch-group", SystemDisplayName: "批量测试分组",
		InternalGroupName: "batch_group", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus:    marketplacedomain.LifecycleActive,
		VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility:         marketplacedomain.VisibilityPublic,
	}).Error)

	view, err := StartBatchMarketplaceTest(7, BatchTestRequest{
		GroupIDs: []string{"batch-group"}, Model: "gpt-5.6-sol",
	})
	require.NoError(t, err)
	require.Equal(t, "user_quota", view.BillingMode)
	require.False(t, view.QuotaCharged)
	require.False(t, view.LogCreated)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, getErr := GetBatchMarketplaceTest(7, view.ID)
		require.NoError(t, getErr)
		if result.Status == "completed" || result.Status == "failed" {
			require.Len(t, result.Items, 1)
			require.NotNil(t, result.Items[0].StartedAt)
			require.NotNil(t, result.Items[0].EndedAt)
			require.GreaterOrEqual(t, result.Items[0].LatencyMS, int64(0))
			// The fixture intentionally has no internal gateway channel, so the
			// request fails before billing. A real upstream success populates these
			// fields from the normal billing/logging pipeline.
			require.False(t, result.Items[0].LogCreated)
			require.Zero(t, result.Items[0].QuotaCharged)
			require.NotEmpty(t, result.Items[0].Error)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("batch test did not finish")
}
