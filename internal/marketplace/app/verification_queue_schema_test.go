package app

import (
	"testing"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestVerificationUpdatesUseDeclaredGroupColumns(t *testing.T) {
	for _, imageOnly := range []bool{false, true} {
		name := "failed_models"
		if imageOnly {
			name = "image_only"
		}
		t.Run(name, func(t *testing.T) {
			db := openMarketplaceAppTestDB(t)
			require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}, &gatewayschema.Channel{}, &gatewayschema.Ability{}))
			internal := gatewayschema.Channel{Key: "test-only", Name: "image"}
			require.NoError(t, db.Create(&internal).Error)
			channel := marketplaceschema.Channel{ID: "channel", OwnerUserID: 42, ProviderType: "openai_compatible", DeclaredModels: `["gpt-image-1"]`, InternalChannelID: &internal.Id, Status: marketplacedomain.LifecycleDraft}
			group := marketplaceschema.Group{ID: "group", ChannelID: channel.ID, OwnerUserID: 42, PublicSlug: "test", InternalGroupName: "market_test", LifecycleStatus: marketplacedomain.LifecycleDraft, VerificationStatus: marketplacedomain.VerificationQueued}
			require.NoError(t, db.Create(&channel).Error)
			require.NoError(t, db.Create(&group).Error)
			require.False(t, db.Migrator().HasColumn(&group, "verification_summary"))
			wantStatus, wantVerification := marketplacedomain.LifecycleDraft, marketplacedomain.VerificationFailed
			if imageOnly {
				require.NoError(t, publishImageOnlyChannel(&channel))
				wantStatus, wantVerification = marketplacedomain.LifecycleActive, marketplacedomain.VerificationPassed
			} else {
				require.NoError(t, persistUnchangedVerificationFailure(&channel))
			}
			require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
			require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
			require.Equal(t, wantStatus, group.LifecycleStatus)
			require.Equal(t, wantStatus, channel.Status)
			require.Equal(t, wantVerification, group.VerificationStatus)
		})
	}
}
