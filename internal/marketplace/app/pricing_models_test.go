package app

import (
	"testing"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestAvailablePricingModelsRequireAccessibleLiveGroupAndAbility(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.Channel{},
		&marketplaceschema.GroupAccess{}, &marketplaceschema.ChannelUserBlock{},
		&gatewayschema.Channel{}, &gatewayschema.Ability{}))
	ids := []string{"public", "private", "invited", "disabled", "unverified", "blocked", "offline", "no-ability", "orphan"}
	for i, id := range ids {
		internalID := i + 1
		group := marketplaceschema.Group{
			ID: id, ChannelID: id, OwnerUserID: 20, PublicSlug: id, InternalGroupName: "market-" + id,
			SourceType: "marketplace_user", Visibility: "public", VerificationStatus: "passed", LifecycleStatus: "active",
		}
		if id == "private" || id == "invited" {
			group.Visibility = "private"
		}
		if id == "disabled" {
			group.LifecycleStatus = "disabled"
		}
		if id == "unverified" {
			group.VerificationStatus = "pending"
		}
		require.NoError(t, db.Create(&group).Error)
		channel := marketplaceschema.Channel{ID: id, OwnerUserID: 20, InternalChannelID: &internalID, DeclaredModels: `["` + id + `"]`}
		require.NoError(t, db.Create(&channel).Error)
		if id != "orphan" {
			status := 1
			if id == "offline" {
				status = 2
			}
			require.NoError(t, db.Create(&gatewayschema.Channel{Id: internalID, Name: id, Status: status}).Error)
		}
		require.NoError(t, db.Create(&gatewayschema.Ability{Group: group.InternalGroupName, Model: id, ChannelId: internalID, Enabled: id != "no-ability"}).Error)
	}
	require.NoError(t, db.Create(&marketplaceschema.GroupAccess{GroupID: "invited", UserID: 10}).Error)
	require.NoError(t, db.Create(&marketplaceschema.ChannelUserBlock{ChannelID: "blocked", UserID: 10}).Error)
	models, err := ListAvailablePricingModels(10)
	require.NoError(t, err)
	require.Equal(t, []string{"invited", "public"}, models)
	models, err = ListAvailablePricingModels(0)
	require.NoError(t, err)
	require.Equal(t, []string{"blocked", "public"}, models, "anonymous visitors must not see private or invite-only models")
	require.NoError(t, db.Model(&marketplaceschema.Group{}).Where("id = ?", "public").Update("lifecycle_status", "disabled").Error)
	models, err = ListAvailablePricingModels(10)
	require.NoError(t, err)
	require.Equal(t, []string{"invited"}, models, "disabling the last group must remove its model")
}
