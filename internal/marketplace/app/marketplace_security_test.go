package app

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

// Parser/concurrency tests explicitly model a platform review response. Normal
// owner connectivity contexts must never carry this server-only marker.
func prepareTrustedPresentationTestProbe(t *testing.T, channel *marketplaceschema.Channel) (context.Context, error) {
	t.Helper()
	ctx, err := preparePresentationProbe(context.Background(), channel)
	return context.WithValue(ctx, trustedPresentationProbeKey{}, true), err
}

func TestMarketplaceDiscountCannotChargeAbovePublicPrice(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &marketplaceschema.MultiplierNotice{}))
	group := seedPublicShopProduct(t, db, "discount-security", 10)
	require.NoError(t, db.Create(&identityschema.User{Id: 20, Username: "discount-consumer"}).Error)
	targets := []MultiplierTarget{{ChannelID: group.ChannelID, UserID: 20}}
	for _, multiplier := range []float64{0.6, 1000000, math.Inf(1), math.NaN(), -0.1} {
		_, err := BatchSetUserMultipliers(10, targets, &multiplier)
		require.Error(t, err)
	}
	var count int64
	require.NoError(t, db.Model(&marketplaceschema.UserMultiplier{}).Count(&count).Error)
	require.Zero(t, count)
	for _, multiplier := range []float64{0.5, 0.25} {
		changed, err := BatchSetUserMultipliers(10, targets, &multiplier)
		require.NoError(t, err)
		require.Equal(t, 1, changed)
		binding, err := ResolveTokenGroupBinding(TokenGroupValue(group.ID), 20)
		require.NoError(t, err)
		require.Equal(t, multiplier, binding.Multiplier)
	}
	// Both legacy malicious overrides and later public reductions are safe.
	for _, override := range []float64{0.25, 999999} {
		require.NoError(t, db.Model(&group).Update("multiplier", 0.1).Error)
		require.NoError(t, db.Model(&marketplaceschema.UserMultiplier{}).Where("channel_id = ?", group.ChannelID).Update("multiplier", override).Error)
		binding, err := ResolveTokenGroupBinding(TokenGroupValue(group.ID), 20)
		require.NoError(t, err)
		require.Equal(t, 0.1, binding.Multiplier)
	}
	_, err := BatchSetUserMultipliers(10, targets, nil)
	require.NoError(t, err)
	for _, override := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		require.Equal(t, 0.1, discountedMarketplaceMultiplier(0.1, override))
	}
}

func TestMarketplaceAdminHoldClosesAbilitiesAndSurvivesOwnerRepair(t *testing.T) {
	for _, action := range []string{"reject", "pause", "legacy_reject"} {
		t.Run(action, func(t *testing.T) {
			db := openMarketplaceAppTestDB(t)
			require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}, &marketplaceschema.VerificationRun{}))
			group := seedPublicShopProduct(t, db, "hold-security", 10)
			internal := gatewayschema.Channel{Key: "test-only", Models: "gpt-5", Group: group.InternalGroupName, Status: constant.ChannelStatusEnabled}
			require.NoError(t, db.Create(&internal).Error)
			require.NoError(t, db.Create(&gatewayschema.Ability{ChannelId: internal.Id, Group: group.InternalGroupName, Model: "gpt-5", Enabled: true}).Error)
			require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", group.ChannelID).Updates(map[string]any{"status": "active", "internal_channel_id": internal.Id, "provider_type": "openai_compatible", "submitted_source_label": "Codex Plus", "source_label_status": "approved"}).Error)
			switch action {
			case "reject":
				_, err := ReviewChannel(group.ChannelID, AdminReviewRequest{Reason: "manual hold"})
				require.NoError(t, err)
			case "pause":
				require.NoError(t, PauseAdminChannel(group.ChannelID, true))
			case "legacy_reject":
				require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", group.ChannelID).Updates(map[string]any{"status": "draft", "source_label_status": "rejected"}).Error)
				require.NoError(t, ReconcileMarketplaceChannels())
			}
			require.Error(t, PauseOwnerChannel(10, group.ChannelID, false))
			label, name := "Codex Pro", "修订渠道"
			_, err := UpdateOwnerChannel(10, group.ChannelID, UpdateChannelRequest{SourceLabel: &label, CustomName: &name})
			require.NoError(t, err)
			channel, current, err := loadChannelGroup(group.ChannelID)
			require.NoError(t, err)
			require.Equal(t, "disabled", channel.Status)
			require.Equal(t, "pending", channel.SourceLabelStatus)
			require.Equal(t, "disabled", current.LifecycleStatus)
			require.NoError(t, db.First(&internal, internal.Id).Error)
			require.Equal(t, constant.ChannelStatusManuallyDisabled, internal.Status)
			var enabled int64
			require.NoError(t, db.Model(&gatewayschema.Ability{}).Where("channel_id = ? AND enabled = ?", internal.Id, true).Count(&enabled).Error)
			require.Zero(t, enabled)
			run := marketplaceschema.VerificationRun{ID: "repair", ChannelID: channel.ID, Status: "running"}
			require.NoError(t, db.Create(&run).Error)
			completeVerification(&run, channel, current, []ModelVerificationResult{{Model: "gpt-5", Listed: true, Status: "passed"}}, nil)
			require.NoError(t, db.First(channel, "id = ?", channel.ID).Error)
			require.Equal(t, "disabled", channel.Status)
			_, err = ReviewChannel(channel.ID, AdminReviewRequest{Approved: true, Reason: "repaired and reviewed"})
			require.NoError(t, err)
			require.NoError(t, db.First(&internal, internal.Id).Error)
			require.Equal(t, constant.ChannelStatusEnabled, internal.Status)
		})
	}
}

func TestMarketplaceAdminRejectionRollsBackIfAbilitiesCannotBeDisabled(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}, &marketplaceschema.VerificationRun{}))
	group := seedPublicShopProduct(t, db, "hold-rollback", 10)
	internal := gatewayschema.Channel{Key: "test-only", Status: constant.ChannelStatusEnabled, Group: group.InternalGroupName, Models: "gpt-5"}
	require.NoError(t, db.Create(&internal).Error)
	require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", group.ChannelID).Updates(map[string]any{"status": "active", "internal_channel_id": internal.Id}).Error)
	require.NoError(t, db.Migrator().DropTable(&gatewayschema.Ability{}))
	_, err := ReviewChannel(group.ChannelID, AdminReviewRequest{Reason: "must be atomic"})
	require.Error(t, err)
	channel, current, err := loadChannelGroup(group.ChannelID)
	require.NoError(t, err)
	require.Equal(t, "active", channel.Status)
	require.Equal(t, "active", current.LifecycleStatus)
	require.NoError(t, db.First(&internal, internal.Id).Error)
	require.Equal(t, constant.ChannelStatusEnabled, internal.Status)
}

func TestMarketplaceReconciliationUsesCurrentTransactionForBackfill(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	platformdb.DB = db.WithContext(ctx)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.VerificationRun{}))
	group := seedPublicShopProduct(t, db, "reconcile-transaction", 10)
	require.NoError(t, db.Model(&group).Updates(map[string]any{"verification_status": "failed", "lifecycle_status": "draft"}).Error)
	require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", group.ChannelID).Updates(map[string]any{"status": "draft", "provider_type": "openai_compatible"}).Error)
	now := time.Now().UTC()
	require.NoError(t, db.Create(&marketplaceschema.VerificationRun{ID: "backfill", ChannelID: group.ChannelID, Status: "failed", DetectorVersion: nativeDetectorVersion, CompletedAt: &now}).Error)
	require.NoError(t, ReconcileMarketplaceChannels())
	channel, _, err := loadChannelGroup(group.ChannelID)
	require.NoError(t, err)
	require.Equal(t, "failed", channel.ConnectivityTestStatus)
	require.Equal(t, "pending", channel.SourceLabelStatus)
	require.Equal(t, "draft", channel.Status)
}

func TestMarketplaceContentEditRevokesCachedDiscoveryImmediately(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldLogDB := platformdb.LogDB
	platformdb.LogDB = nil
	t.Cleanup(func() { platformdb.LogDB = oldLogDB })
	require.NoError(t, db.AutoMigrate(&marketplaceschema.RankingSnapshot{}, &marketplaceschema.ChannelFeedback{}, &marketplaceschema.GroupAccess{}, &gatewayschema.Ability{}, &gatewayschema.Channel{}))
	group := seedPublicShopProduct(t, db, "cached-content", 10)
	require.NoError(t, db.Create(&marketplaceschema.RankingSnapshot{GroupID: group.ID, WindowHours: 24, RankingVersion: rankingVersion, CalculatedAt: time.Now()}).Error)
	query := GroupQuery{ShopID: 10}
	before, err := ListMarketplaceGroups(query)
	require.NoError(t, err)
	require.Equal(t, 1, before.Total)
	name := "重新审核"
	_, err = UpdateOwnerChannel(10, group.ChannelID, UpdateChannelRequest{CustomName: &name})
	require.NoError(t, err)
	after, err := ListMarketplaceGroups(query)
	require.NoError(t, err)
	require.Zero(t, after.Total)
	require.Empty(t, after.Items)
}

func TestMarketplaceImageOnlyChannelRequiresRealImageResponse(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty_image", true: "actual_image"}[valid], func(t *testing.T) {
			db := openMarketplaceAppTestDB(t)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}, &marketplaceschema.VerificationRun{}))
			group := seedPublicShopProduct(t, db, "image-queue", 10)
			seen, release := make(chan struct{}, 1), make(chan struct{})
			var released sync.Once
			defer func() {
				released.Do(func() { close(release) })
				require.Eventually(t, func() bool {
					return !marketplaceVerificationTasks.active(group.ChannelID, verificationTaskConnectivity)
				}, 5*time.Second, 10*time.Millisecond)
			}()
			server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"data":[{"id":"gpt-image-1"}]}`))
					return
				}
				require.Equal(t, "/v1/images/generations", r.URL.Path)
				select {
				case seen <- struct{}{}:
				default:
				}
				<-release
				if valid {
					_, _ = w.Write([]byte(`{"data":[{"b64_json":"aW1hZ2U="}]}`))
				} else {
					_, _ = w.Write([]byte(`{"data":[]}`))
				}
			}))
			require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", group.ChannelID).Updates(map[string]any{"base_url_ciphertext": server.URL, "credential_ciphertext": "isolated-image-test", "declared_models": `["gpt-image-1"]`, "provider_type": "openai_compatible", "status": "draft"}).Error)
			require.NoError(t, db.Model(&group).Updates(map[string]any{"custom_name": "", "tags": "[]", "lifecycle_status": "draft", "verification_status": "queued"}).Error)
			require.NoError(t, QueueRequiredVerification(group.ChannelID))
			select {
			case <-seen:
			case <-time.After(5 * time.Second):
				t.Fatal("image probe did not send a real generation request")
			}
			require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
			require.NotEqual(t, "active", group.LifecycleStatus)
			released.Do(func() { close(release) })
			require.Eventually(t, func() bool {
				return !marketplaceVerificationTasks.active(group.ChannelID, verificationTaskConnectivity)
			}, 5*time.Second, 10*time.Millisecond)
			require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
			if valid {
				require.Equal(t, "passed", group.VerificationStatus)
				require.Equal(t, "active", group.LifecycleStatus)
			} else {
				require.Equal(t, "failed", group.VerificationStatus)
				require.NotEqual(t, "active", group.LifecycleStatus)
			}
		})
	}
}

func TestMarketplacePendingContentHiddenFromDiscoveryAndBinding(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	group := seedPublicShopProduct(t, db, "pending-security", 10)
	for _, status := range []string{"pending", "failed", "rule_passed", ""} {
		require.NoError(t, db.Model(&group).Update("moderation_status", status).Error)
		var count int64
		require.NoError(t, publicGroupsQuery(GroupQuery{}).Count(&count).Error)
		require.Zero(t, count, status)
		_, err := ResolveTokenGroupBinding(TokenGroupValue(group.ID), 20)
		require.Error(t, err, status)
	}
	require.NoError(t, db.Model(&group).Updates(map[string]any{"custom_name": "", "description": "", "tags": "[]", "moderation_status": ""}).Error)
	var count int64
	require.NoError(t, publicGroupsQuery(GroupQuery{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "legacy content-free channels stay available")
	require.NoError(t, db.Create(&marketplaceschema.Shop{OwnerUserID: 10, Name: "待审名称", Description: "待审描述", ModerationStatus: "pending"}).Error)
	views, err := attachShopPresentation([]marketplaceschema.Group{group})
	require.NoError(t, err)
	require.NotEqual(t, "待审名称", views[10].Name)
}

func TestMarketplaceReviewUsesOnlyPlatformEndpointAndUnknownStaysPending(t *testing.T) {
	for _, verdict := range []string{"SAFE", "AD", "SAFE because no ads", "请求失败"} {
		t.Run(verdict, func(t *testing.T) {
			db := openMarketplaceAppTestDB(t)
			group := seedPublicShopProduct(t, db, "trusted-review", 10)
			calls := 0
			server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "Bearer platform-test-only", r.Header.Get("Authorization"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				encoded, err := json.Marshal(body)
				require.NoError(t, err)
				require.Contains(t, string(encoded), "推理渠道")
				require.Contains(t, string(encoded), "不可信")
				_, _ = w.Write(presentationTextResponse(t, verdict))
			}))
			t.Setenv("PROMPT_AUDIT_MODE", "blocking")
			t.Setenv("PROMPT_AUDIT_ENDPOINTS_JSON", "")
			t.Setenv("PROMPT_AUDIT_BASE_URL", server.URL)
			t.Setenv("PROMPT_AUDIT_API_KEY", "platform-test-only")
			t.Setenv("PROMPT_AUDIT_MODEL", "gpt-5")
			ctx, err := preparePresentationProbe(context.Background(), &marketplaceschema.Channel{ID: group.ChannelID})
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
			want := map[string]string{"SAFE": "passed", "AD": "failed"}[verdict]
			if want == "" {
				want = "pending"
			}
			require.Equal(t, want, group.ModerationStatus)
			for _, upstreamVerdict := range []string{"SAFE", "AD"} {
				require.NoError(t, validatePresentationProbeResponse(ctx, "openai_compatible", "gpt-5", presentationTextResponse(t, upstreamVerdict)))
			}
			require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
			require.Equal(t, want, group.ModerationStatus, "owner responses cannot change a platform decision")
		})
	}
}
