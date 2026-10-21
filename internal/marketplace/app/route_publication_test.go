package app

import (
	"testing"

	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestVerifiedPublicationSynchronizesGroupAbilities(t *testing.T) {
	for _, flow := range []string{"reconcile", "verification", "incremental"} {
		for _, state := range []string{"stale_pause", "missing_abilities", "owner_pause", "group_pause", "admin_hold", "group_hold", "source_rejected"} {
			t.Run(flow+"/"+state, func(t *testing.T) {
				db := openMarketplaceAppTestDB(t)
				require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}, &marketplaceschema.VerificationRun{}))
				group := seedPublicShopProduct(t, db, "route-publication", 10)
				internal := gatewayschema.Channel{Key: "test-only", Models: "gpt-5", Group: group.InternalGroupName, Status: constant.ChannelStatusManuallyDisabled}
				if state != "stale_pause" {
					internal.Status = constant.ChannelStatusEnabled
				}
				require.NoError(t, db.Create(&internal).Error)
				if state == "stale_pause" {
					require.NoError(t, gatewaystore.AddChannelAbilities(&internal, db))
				}
				require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", group.ChannelID).Updates(map[string]any{
					"status": "active", "internal_channel_id": internal.Id, "provider_type": "openai_compatible", "submitted_source_label": "自营 Pro 号池", "source_label_status": "pending", "connectivity_test_status": "passed",
				}).Error)
				channel, current, err := loadChannelGroup(group.ChannelID)
				require.NoError(t, err)
				// Keep stale in-memory values to exercise the locked lifecycle reread.
				switch state {
				case "owner_pause":
					require.NoError(t, db.Model(channel).Update("status", "suspended").Error)
				case "group_pause":
					require.NoError(t, db.Model(current).Update("lifecycle_status", "suspended").Error)
				case "admin_hold":
					require.NoError(t, db.Model(channel).Update("status", "disabled").Error)
				case "group_hold":
					require.NoError(t, db.Model(current).Update("lifecycle_status", "disabled").Error)
				case "source_rejected":
					require.NoError(t, db.Model(channel).Update("source_label_status", "rejected").Error)
				}
				run := marketplaceschema.VerificationRun{ID: "publication-run", ChannelID: channel.ID, Status: "running", DetectorVersion: nativeDetectorVersion}
				require.NoError(t, db.Create(&run).Error)
				switch flow {
				case "reconcile":
					require.NoError(t, ReconcileMarketplaceChannels())
				case "verification":
					completeVerification(&run, channel, current, []ModelVerificationResult{{Model: "gpt-5", Listed: true, Status: "passed"}}, nil)
				case "incremental":
					require.NoError(t, publishVerifiedConnectivity(channel))
				}
				require.NoError(t, db.First(&internal, internal.Id).Error)
				require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
				require.NoError(t, db.First(channel, "id = ?", channel.ID).Error)
				require.Equal(t, "自营 Pro 号池", channel.SubmittedSourceLabel)
				models := gatewaystore.LoadGroupEnabledModels(group.InternalGroupName)
				if state == "stale_pause" || state == "missing_abilities" {
					require.Equal(t, constant.ChannelStatusEnabled, internal.Status)
					require.Equal(t, "active", group.LifecycleStatus)
					require.Equal(t, []string{"gpt-5"}, models)
				} else {
					require.Equal(t, constant.ChannelStatusManuallyDisabled, internal.Status)
					require.Empty(t, models)
					if state == "group_pause" {
						require.Equal(t, "suspended", group.LifecycleStatus)
					}
					if state == "group_hold" {
						require.Equal(t, "disabled", group.LifecycleStatus)
					}
				}
			})
		}
	}
}

func TestVerifiedPublicationRollsBackWhenEnablingAbilitiesFails(t *testing.T) {
	for _, flow := range []string{"reconcile", "verification", "incremental"} {
		t.Run(flow, func(t *testing.T) {
			db := openMarketplaceAppTestDB(t)
			require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}, &marketplaceschema.VerificationRun{}))
			group := seedPublicShopProduct(t, db, "publication-rollback", 10)
			internal := gatewayschema.Channel{Key: "test-only", Models: "gpt-5", Group: group.InternalGroupName, Status: constant.ChannelStatusManuallyDisabled}
			require.NoError(t, db.Create(&internal).Error)
			require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", group.ChannelID).Updates(map[string]any{
				"status": "draft", "internal_channel_id": internal.Id, "provider_type": "openai_compatible", "submitted_source_label": "Codex Plus", "connectivity_test_status": "passed",
			}).Error)
			require.NoError(t, db.Model(&group).Update("lifecycle_status", "draft").Error)
			channel, current, err := loadChannelGroup(group.ChannelID)
			require.NoError(t, err)
			run := marketplaceschema.VerificationRun{ID: "rollback-run", ChannelID: channel.ID, Status: "running", DetectorVersion: nativeDetectorVersion}
			require.NoError(t, db.Create(&run).Error)
			require.NoError(t, db.Exec(`CREATE TRIGGER fail_enable BEFORE INSERT ON abilities WHEN NEW.enabled = 1 BEGIN SELECT RAISE(FAIL, 'ability write failed'); END`).Error)
			switch flow {
			case "reconcile":
				require.Error(t, ReconcileMarketplaceChannels())
			case "verification":
				completeVerification(&run, channel, current, []ModelVerificationResult{{Model: "gpt-5", Listed: true, Status: "passed"}}, nil)
				require.NoError(t, db.First(&run, "id = ?", run.ID).Error)
				require.Equal(t, "running", run.Status)
			case "incremental":
				require.Error(t, publishVerifiedConnectivity(channel))
			}
			require.NoError(t, db.First(channel, "id = ?", channel.ID).Error)
			require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
			require.NoError(t, db.First(&internal, internal.Id).Error)
			require.Equal(t, "draft", channel.Status)
			require.Equal(t, "draft", group.LifecycleStatus)
			require.Equal(t, constant.ChannelStatusManuallyDisabled, internal.Status)
			require.Empty(t, gatewaystore.LoadGroupEnabledModels(group.InternalGroupName))
		})
	}
}
