package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestStaleChannelPatchKeepsNewReviewAndLifecycle(t *testing.T) {
	for _, review := range []string{"failed", "pending"} {
		t.Run(review, func(t *testing.T) {
			db := openMarketplaceAppTestDB(t)
			require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.VerificationRun{}))
			group := seedPublicShopProduct(t, db, "stale-patch", 10)
			channel, stale, err := loadOwnedChannelGroup(10, group.ChannelID)
			require.NoError(t, err)
			require.NoError(t, db.Model(&group).Updates(map[string]any{"moderation_status": review, "moderation_reason": "new decision", "lifecycle_status": "suspended"}).Error)
			require.NoError(t, db.Model(channel).Update("status", "suspended").Error)
			capacity := 7
			_, err = updateMarketplaceChannel(channel, stale, UpdateChannelRequest{MaxConcurrency: &capacity}, nil)
			require.NoError(t, err)
			require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
			require.Equal(t, review, group.ModerationStatus)
			require.Equal(t, "new decision", group.ModerationReason)
			require.Equal(t, "suspended", group.LifecycleStatus)
			require.NoError(t, db.First(channel, "id = ?", channel.ID).Error)
			require.Equal(t, "suspended", channel.Status)
			require.Equal(t, capacity, channel.MaxConcurrency)
		})
	}
}

func TestLateVerificationAndSyncCannotPublishPausedInternalChannel(t *testing.T) {
	for _, state := range []string{"suspended", "disabled"} {
		t.Run(state, func(t *testing.T) {
			db := openMarketplaceAppTestDB(t)
			require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.VerificationRun{}, &gatewayschema.Channel{}, &gatewayschema.Ability{}))
			group := seedPublicShopProduct(t, db, "late-internal", 10)
			channel, _, err := loadOwnedChannelGroup(10, group.ChannelID)
			require.NoError(t, err)
			channel.Status = "active"
			require.NoError(t, db.Model(channel).Update("status", state).Error)
			require.NoError(t, createInternalChannel(channel, &group))
			require.Nil(t, channel.InternalChannelID)
			run := marketplaceschema.VerificationRun{ID: "late-internal-run", ChannelID: channel.ID, Status: "running"}
			require.NoError(t, db.Create(&run).Error)
			completeVerification(&run, channel, &group, []ModelVerificationResult{{Model: "gpt-5", Listed: true, Status: "passed"}}, nil)
			require.NoError(t, db.First(channel, "id = ?", channel.ID).Error)
			require.Nil(t, channel.InternalChannelID)
			require.Equal(t, state, channel.Status)
			var count int64
			require.NoError(t, db.Model(&gatewayschema.Channel{}).Count(&count).Error)
			require.Zero(t, count)
			internal := gatewayschema.Channel{Key: "test", Status: constant.ChannelStatusManuallyDisabled, Models: "gpt-5", Group: "test"}
			require.NoError(t, db.Create(&internal).Error)
			channel.InternalChannelID = &internal.Id
			require.NoError(t, syncInternalChannel(channel, &group))
			require.NoError(t, db.First(&internal, internal.Id).Error)
			require.Equal(t, constant.ChannelStatusManuallyDisabled, internal.Status)
			var enabled int64
			require.NoError(t, db.Model(&gatewayschema.Ability{}).Where("channel_id = ? AND enabled = ?", internal.Id, true).Count(&enabled).Error)
			require.Zero(t, enabled)
		})
	}
}

func TestCodexContradictoryPresentationFieldsRemainPending(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}))
	group := seedPublicShopProduct(t, db, "contradictory-fields", 10)
	ctx, err := prepareTrustedPresentationTestProbe(t, &marketplaceschema.Channel{ID: group.ChannelID})
	require.NoError(t, err)
	require.NoError(t, validatePresentationProbeResponse(ctx, "codex", "gpt-5", []byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"AD"}]}],"output_text":"SAFE"}`)))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "pending", group.ModerationStatus)
}

func TestStaleOwnerLifecycleActionCannotUndoAdministratorDisable(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}))
	group := seedPublicShopProduct(t, db, "late-owner-resume", 10)
	channel, stale, err := loadOwnedChannelGroup(10, group.ChannelID)
	require.NoError(t, err)
	require.NoError(t, db.Model(channel).Update("status", "disabled").Error)
	for _, paused := range []bool{true, false} {
		require.ErrorContains(t, setMarketplaceChannelLifecycle(channel, stale, "active", paused, true), "已停用")
	}
	require.NoError(t, db.First(channel, "id = ?", channel.ID).Error)
	require.Equal(t, "disabled", channel.Status)
}

func TestPresentationRulesDistinguishTechnicalMetadataFromContact(t *testing.T) {
	for _, value := range []string{"HTTP streaming", "HTTPS API", "AVX512 support", "2026-10-17", "20261017", "Claude Sonnet 20250219", "claude-3-7-sonnet-20250219", "Qwen3-235B-A22B-2507", "23:00-01:00", "GPT-5.6 编程", "低延迟 HTTP 接口"} {
		got, err := normalizePublicText(value, 300, false)
		require.NoError(t, err, value)
		require.Equal(t, value, got)
	}
	for _, value := range []string{"https://example.com", "h t t p s : / / example . com", "hxxps://example.net", "ｗｗｗ．ｅｘａｍｐｌｅ．ｃｏｍ", "support@example.com", "example . com", "t . me / someone", "微 信 abc", "微\n信abc", "加-群", "VX123456", "vxaccount", "加 v x abc", "138 1234 5678", "+86 138-1234-5678", "123 456 789", "12345678", "测\u200b试"} {
		_, err := normalizePublicText(value, 300, false)
		require.Error(t, err, value)
	}
}

func TestPresentationVerdictAcceptsOnlyOneUnambiguousConclusion(t *testing.T) {
	for _, value := range []string{"SAFE", " safe ", "SAFE。", "SAFE.", "**SAFE**", "`SAFE`", `"SAFE"`, "[SAFE]", "```\nSAFE\n```", "```text\nSAFE\n```", "```json\n{\"verdict\":\"SAFE\"}\n```", `{"result":"SAFE"}`, `{"status":"safe"}`} {
		require.Equal(t, "SAFE", parsePresentationVerdict(value), value)
	}
	for _, value := range []string{"AD", "ad.", "**AD**", `{"verdict":"AD"}`, "```text\nAD\n```"} {
		require.Equal(t, "AD", parsePresentationVerdict(value), value)
	}
	for _, value := range []string{"", "OK", "SAFE because no ads", "SAFE AD", "AD SAFE", "not AD", "SAFE\nAD", `{"verdict":"AD","verdict":"SAFE"}`, `{"verdict":"SAFE","reason":"looks fine"}`, `{"verdict":"SAFE","ad":true}`, `{"verdict":true}`, `{"verdict":"SAFE"} {"verdict":"AD"}`, `["SAFE"]`, "```json\nSAFE\n``` extra", "```python\nSAFE\n```", strings.Repeat(" ", 256) + "SAFE" + strings.Repeat("x", 256)} {
		require.Empty(t, parsePresentationVerdict(value), value)
	}
}

func presentationTextResponse(t *testing.T, text string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
	require.NoError(t, err)
	return body
}

func TestPresentationInconclusiveVerdictPreservesConnectivityAndErrorDetection(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}))
	group := seedPublicShopProduct(t, db, "pending-review", 10)
	for _, item := range []struct{ body, wantError string }{
		{`{"choices":[{"message":{"content":"SAFE because no ads"}}]}`, ""},
		{`{"error":{"message":"quota exceeded"}}`, "上游返回失败内容"},
		{`{"choices":[{"message":{"content":"请求失败，请重试"}}]}`, "探针模型返回失败内容"},
		{`{"choices":[]}`, "缺少有效的模型输出"},
		{`not json`, "不是有效 JSON"},
	} {
		ctx, err := prepareTrustedPresentationTestProbe(t, &marketplaceschema.Channel{ID: group.ChannelID})
		require.NoError(t, err)
		requests := 0
		server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++; _, _ = w.Write([]byte(item.body)) }))
		_, err = probeMarketplaceInferenceTimedContext(ctx, "openai_compatible", server.URL, "local-test-only", "gpt-5")
		server.Close()
		if item.wantError == "" {
			require.NoError(t, err)
			require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
			require.Equal(t, "pending", group.ModerationStatus)
			require.Contains(t, group.ModerationReason, "未返回有效")
		} else {
			require.ErrorContains(t, err, item.wantError)
		}
		require.Equal(t, 1, requests)
	}
}

func TestPresentationNewSafeRecoversSemanticFailureButNotNewerADOrRuleFailure(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}))
	group := seedPublicShopProduct(t, db, "semantic-recovery", 10)
	require.NoError(t, db.Create(&marketplaceschema.Shop{OwnerUserID: 10, Name: "模型店"}).Error)
	prepare := func() context.Context {
		ctx, err := prepareTrustedPresentationTestProbe(t, &marketplaceschema.Channel{ID: group.ChannelID})
		require.NoError(t, err)
		return ctx
	}
	old := prepare()
	ad := prepare()
	require.ErrorContains(t, validatePresentationProbeResponse(ad, "openai_compatible", "gpt-5", presentationTextResponse(t, "AD.")), "模型广告检查未通过")
	require.Error(t, validatePresentationProbeResponse(ad, "openai_compatible", "gpt-5", presentationTextResponse(t, "SAFE")))
	require.NoError(t, validatePresentationProbeResponse(old, "openai_compatible", "gpt-5", presentationTextResponse(t, "SAFE")))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "failed", group.ModerationStatus, "an older concurrent SAFE must not erase a newer AD")

	fresh := prepare()
	require.NoError(t, validatePresentationProbeResponse(fresh, "openai_compatible", "gpt-5", presentationTextResponse(t, `{"verdict":"SAFE"}`)))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "passed", group.ModerationStatus)
	var shop marketplaceschema.Shop
	require.NoError(t, db.First(&shop, "owner_user_id = ?", 10).Error)
	require.Equal(t, "passed", shop.ModerationStatus)
	// Both probes observed the same earlier rejection. A second AD must
	// advance the decision even when the host clock has coarse resolution.
	require.NoError(t, db.Model(&group).Updates(map[string]any{"moderation_status": "failed", "moderation_reason": presentationSemanticADReason, "moderation_checked_at": time.Now().UTC().Add(time.Hour)}).Error)
	olderRetry, newerRetry := prepare(), prepare()
	require.Error(t, validatePresentationProbeResponse(newerRetry, "openai_compatible", "gpt-5", presentationTextResponse(t, "AD")))
	require.NoError(t, validatePresentationProbeResponse(olderRetry, "openai_compatible", "gpt-5", presentationTextResponse(t, "SAFE")))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "failed", group.ModerationStatus)

	require.NoError(t, db.Model(&group).Updates(map[string]any{"moderation_status": "failed", "moderation_reason": "名称、备注和标签不能包含网址或联系方式", "moderation_checked_at": time.Now().UTC()}).Error)
	fresh = prepare()
	require.NoError(t, validatePresentationProbeResponse(fresh, "openai_compatible", "gpt-5", presentationTextResponse(t, "SAFE")))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "failed", group.ModerationStatus, "a SAFE must not erase a non-semantic restriction")
}

func TestPresentationEditAndUndoStillInvalidatesOldProbe(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.VerificationRun{}, &marketplaceschema.Settlement{}))
	group := seedPublicShopProduct(t, db, "undo-review", 10)
	_, err := UpdateOwnerShop(10, ShopProfileRequest{Name: "原店铺"})
	require.NoError(t, err)
	ctx, err := prepareTrustedPresentationTestProbe(t, &marketplaceschema.Channel{ID: group.ChannelID})
	require.NoError(t, err)
	for _, name := range []string{"临时店铺", "原店铺"} {
		_, err = UpdateOwnerShop(10, ShopProfileRequest{Name: name})
		require.NoError(t, err)
	}
	require.Error(t, validatePresentationProbeResponse(ctx, "openai_compatible", "gpt-5", presentationTextResponse(t, "AD")))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "pending", group.ModerationStatus)
	ctx, err = prepareTrustedPresentationTestProbe(t, &marketplaceschema.Channel{ID: group.ChannelID})
	require.NoError(t, err)
	for _, name := range []string{"临时渠道", group.CustomName} {
		_, err = UpdateOwnerChannel(10, group.ChannelID, UpdateChannelRequest{CustomName: &name})
		require.NoError(t, err)
	}
	require.Error(t, validatePresentationProbeResponse(ctx, "openai_compatible", "gpt-5", presentationTextResponse(t, "AD")))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "pending", group.ModerationStatus)
}

func TestShopEditReopensOnlyOwnedGroupReviewAndRejectsStaleProbe(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Settlement{}))
	group := seedPublicShopProduct(t, db, "shop-recovery", 10)
	other := seedPublicShopProduct(t, db, "other-owner", 20)
	require.NoError(t, db.Model(&group).Updates(map[string]any{"moderation_status": "failed", "moderation_reason": presentationSemanticADReason, "lifecycle_status": "suspended"}).Error)
	ctx, err := prepareTrustedPresentationTestProbe(t, &marketplaceschema.Channel{ID: group.ChannelID})
	require.NoError(t, err)
	_, err = UpdateOwnerShop(10, ShopProfileRequest{Name: "新模型店", Description: "HTTP streaming 与 AVX512"})
	require.NoError(t, err)
	require.NoError(t, validatePresentationProbeResponse(ctx, "openai_compatible", "gpt-5", presentationTextResponse(t, "SAFE")))
	for _, verdict := range []string{"SAFE", "AD"} {
		probe := ctx.Value(presentationProbeKey{}).(*presentationProbe)
		require.NoError(t, probe.record(context.Background(), map[string]string{"SAFE": "passed", "AD": "failed"}[verdict], verdict))
	}
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "pending", group.ModerationStatus)
	require.Equal(t, "suspended", group.LifecycleStatus)
	require.NoError(t, db.First(&other, "id = ?", other.ID).Error)
	require.Equal(t, "passed", other.ModerationStatus)
	newCtx, err := prepareTrustedPresentationTestProbe(t, &marketplaceschema.Channel{ID: group.ChannelID})
	require.NoError(t, err)
	require.NoError(t, validatePresentationProbeResponse(newCtx, "openai_compatible", "gpt-5", presentationTextResponse(t, "SAFE")))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "passed", group.ModerationStatus)
}

func TestOwnerCanEditPresentationAcrossFailedAndRestrictedStatesWithoutOtherValidation(t *testing.T) {
	for _, state := range []string{"draft", "active", "suspended", "disabled"} {
		t.Run(state, func(t *testing.T) {
			db := openMarketplaceAppTestDB(t)
			require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.VerificationRun{}))
			group := seedPublicShopProduct(t, db, "owner-edit", 10)
			require.NoError(t, db.Model(&group).Updates(map[string]any{"moderation_status": "failed", "verification_status": "failed", "lifecycle_status": state}).Error)
			require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", group.ChannelID).Updates(map[string]any{"status": state, "connectivity_test_status": "failed", "model_prices": "legacy-price", "auto_probe_enabled": true, "auto_probe_interval_minutes": -1, "auto_probe_model": "old-missing-model", "submitted_source_label": "旧来源"}).Error)
			name, note, tags := "修订模型", "HTTP streaming", []string{"AVX512", "20261017"}
			req := UpdateChannelRequest{CustomName: &name, Description: &note, Tags: &tags}
			_, err := UpdateOwnerChannel(20, group.ChannelID, req)
			require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			_, err = UpdateOwnerChannel(10, group.ChannelID, req)
			require.NoError(t, err)
			require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
			require.Equal(t, "pending", group.ModerationStatus)
			require.Equal(t, name, group.CustomName)
			require.Equal(t, note, group.Description)
			require.Equal(t, state, group.LifecycleStatus)
			require.Equal(t, "failed", group.VerificationStatus)
			var channel marketplaceschema.Channel
			require.NoError(t, db.First(&channel, "id = ?", group.ChannelID).Error)
			require.Equal(t, state, channel.Status)
			require.Equal(t, "legacy-price", channel.ModelPrices)
			require.Equal(t, -1, channel.AutoProbeIntervalMinutes)
		})
	}
}

func TestPresentationReviewUsesOneTextModelAndPreservesPausedLifecycle(t *testing.T) {
	for _, status := range []string{"pending", "failed"} {
		require.Equal(t, []string{"gpt-5"}, presentationReviewModels(&marketplaceschema.Group{ModerationStatus: status}, []string{"text-embedding-3-small", "gpt-image-2", "gemini-3.1-flash-image-preview", "gpt-5", "gpt-4o"}))
	}
	require.Empty(t, presentationReviewModels(&marketplaceschema.Group{ModerationStatus: "passed"}, []string{"gpt-5"}))
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.VerificationRun{}))
	group := seedPublicShopProduct(t, db, "paused-review", 10)
	require.NoError(t, db.Model(&group).Update("lifecycle_status", "suspended").Error)
	require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", group.ChannelID).Update("status", "suspended").Error)
	run := marketplaceschema.VerificationRun{ID: "paused-native", ChannelID: group.ChannelID, Status: "queued"}
	require.NoError(t, prepareConnectivityTest(&run, nil))
	var channel marketplaceschema.Channel
	require.NoError(t, db.First(&channel, "id = ?", group.ChannelID).Error)
	require.Equal(t, "suspended", channel.Status)
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "suspended", group.LifecycleStatus)
	require.NoError(t, db.Model(&run).Update("status", "running").Error)
	completeVerification(&run, &channel, &group, []ModelVerificationResult{{Model: "gpt-5", Listed: true, Status: marketplacedomain.ModelVerificationPassed}}, nil)
	require.NoError(t, db.First(&channel, "id = ?", group.ChannelID).Error)
	require.Equal(t, "suspended", channel.Status)
	require.Equal(t, "passed", channel.ConnectivityTestStatus)
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "suspended", group.LifecycleStatus)
	require.NoError(t, db.Model(&channel).Update("status", "disabled").Error)
	for _, paused := range []bool{true, false} {
		require.ErrorContains(t, PauseOwnerChannel(10, group.ChannelID, paused), "已停用")
	}

	// A late successful result cannot undo an administrator disable.
	late := marketplaceschema.VerificationRun{ID: "late-native", ChannelID: group.ChannelID, Status: "running"}
	require.NoError(t, db.Create(&late).Error)
	channel.Status = "suspended"
	completeVerification(&late, &channel, &group, []ModelVerificationResult{{Model: "gpt-5", Listed: true, Status: "passed"}}, nil)
	require.NoError(t, db.First(&channel, "id = ?", group.ChannelID).Error)
	require.Equal(t, "disabled", channel.Status)
	blocked := marketplaceschema.VerificationRun{ID: "blocked-native", ChannelID: group.ChannelID, Status: "queued"}
	require.NoError(t, prepareConnectivityTest(&blocked, nil))
	require.NoError(t, db.First(&channel, "id = ?", group.ChannelID).Error)
	require.Equal(t, "disabled", channel.Status, "repair evidence must not release the administrator hold")
	var count int64
	require.NoError(t, db.Model(&marketplaceschema.VerificationRun{}).Where("id = ?", blocked.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
