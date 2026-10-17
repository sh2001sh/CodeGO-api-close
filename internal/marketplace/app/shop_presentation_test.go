package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedPublicShopProduct(t *testing.T, db *gorm.DB, id string, owner int) marketplaceschema.Group {
	t.Helper()
	group := marketplaceschema.Group{ID: id, ChannelID: id, OwnerUserID: owner, InternalGroupName: id, PublicSlug: id, CustomName: "推理渠道", Tags: `["编程","低延迟"]`, Visibility: "public", VerificationStatus: "passed", LifecycleStatus: "active", Multiplier: 0.5}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: id, OwnerUserID: owner, DeclaredModels: `["gpt-5"]`}).Error)
	return group
}

func TestShopListingAggregatesBeyondProductPageAndFilters(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Settlement{}))
	for index := range 25 {
		seedPublicShopProduct(t, db, fmt.Sprintf("product-%02d", index), 10)
	}
	private := seedPublicShopProduct(t, db, "private", 20)
	require.NoError(t, db.Model(&private).Update("visibility", "private").Error)
	failed := seedPublicShopProduct(t, db, "failed", 30)
	require.NoError(t, db.Model(&failed).Update("moderation_status", "failed").Error)
	deleted := seedPublicShopProduct(t, db, "deleted", 40)
	require.NoError(t, db.Delete(&marketplaceschema.Channel{}, "id = ?", deleted.ChannelID).Error)
	_, err := UpdateOwnerShop(10, ShopProfileRequest{Name: "晨光模型店", Description: "提供稳定的编程模型"})
	require.NoError(t, err)
	for _, query := range []GroupQuery{{}, {Search: "晨光"}, {Tag: "编程"}, {Search: "稳定的"}, {Search: "10"}} {
		shops, err := ListMarketplaceShops(query)
		require.NoError(t, err)
		require.Equal(t, 1, shops.Total)
		require.Equal(t, 25, shops.Items[0].ChannelCount)
		require.Equal(t, "晨光模型店", shops.Items[0].Name)
		require.ElementsMatch(t, []string{"编程", "低延迟"}, shops.Items[0].Tags)
	}
	for _, search := range []string{"gpt-5", "推理渠道", "低延迟", "product-00"} {
		shops, err := ListMarketplaceShops(GroupQuery{Search: search})
		require.NoError(t, err)
		require.Zero(t, shops.Total, search)
	}
	shops, err := ListMarketplaceShops(GroupQuery{Tag: "不存在"})
	require.NoError(t, err)
	require.Zero(t, shops.Total)
	shops, err = ListMarketplaceShops(GroupQuery{Page: math.MaxInt})
	require.NoError(t, err)
	require.Empty(t, shops.Items)
	_, err = UpdateOwnerShop(11, ShopProfileRequest{Name: "另一个店铺"})
	require.NoError(t, err)
	owner, err := GetOwnerShop(10)
	require.NoError(t, err)
	require.Equal(t, "晨光模型店", owner.CustomName)
}

func TestShopRatingRequiresUsageAndReplacesVote(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Settlement{}))
	seedPublicShopProduct(t, db, "rating-product", 10)
	_, err := RateMarketplaceShop(10, 10, 5)
	require.Error(t, err)
	_, err = RateMarketplaceShop(20, 10, 5)
	require.Error(t, err)
	_, err = RateMarketplaceShop(0, 10, 5)
	require.Error(t, err)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{ID: "consumed", RequestID: "consumed", GroupID: "rating-product", OwnerUserID: 10, ConsumerUserID: 20, ConsumerAmount: 100}).Error)
	for _, stars := range []int{0, 6, -1} {
		_, err = RateMarketplaceShop(20, 10, stars)
		require.Error(t, err)
	}
	view, err := RateMarketplaceShop(20, 10, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, view.Rating)
	require.EqualValues(t, 1, view.RatingCount)
	view, err = RateMarketplaceShop(20, 10, 5)
	require.NoError(t, err)
	require.EqualValues(t, 5, view.Rating)
	require.EqualValues(t, 1, view.RatingCount)
	require.Equal(t, 5, view.ViewerStars)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{ID: "second", RequestID: "second", GroupID: "rating-product", OwnerUserID: 10, ConsumerUserID: 21, ConsumerAmount: 1}).Error)
	view, err = RateMarketplaceShop(21, 10, 3)
	require.NoError(t, err)
	require.EqualValues(t, 4, view.Rating)
	require.EqualValues(t, 2, view.RatingCount)
}

func TestPublicPresentationRejectsAdsAndKeepsFailedVerdict(t *testing.T) {
	for _, text := range []string{"https://example.com", "ｗｗｗ．ｅｘａｍｐｌｅ．ｃｏｍ", "微 信 abc", "微\n信 abc", "加-群", "VX123456", "123 456 789", "测试\u200b名称", "访问 example . com", "私信我购买"} {
		_, err := normalizePublicText(text, 300, false)
		require.Error(t, err, text)
	}
	for _, text := range []string{"GPT-5 编程", "GPT-5.6 编程", "Claude 3.7", "C++", "低延迟", "提供推理与代码能力", "模型 128K 上下文"} {
		_, err := normalizePublicText(text, 300, false)
		require.NoError(t, err, text)
	}
	tags, err := normalizeGroupTags([]string{"编程", "GPT", "gpt", " 编程 "})
	require.NoError(t, err)
	require.Equal(t, []string{"编程", "GPT"}, tags)
	_, err = normalizeGroupTags([]string{"1", "2", "3", "4", "5", "6", "7"})
	require.Error(t, err)
	group := marketplaceschema.Group{ID: "group-42", ChannelID: "42", PublicSlug: "original-slug", SystemDisplayName: "系统名称", CustomName: "原名称", ModerationStatus: "failed", ModerationReason: "广告"}
	name := "原名称"
	require.NoError(t, applyPublicPresentation(&group, &name, nil, nil))
	require.Equal(t, "failed", group.ModerationStatus)
	name = "新名称"
	require.NoError(t, applyPublicPresentation(&group, &name, nil, nil))
	require.Equal(t, "pending", group.ModerationStatus)
	require.Equal(t, "group-42", group.ID)
	require.Equal(t, "42", group.ChannelID)
	require.Equal(t, "original-slug", group.PublicSlug)
	require.Equal(t, "系统名称", group.SystemDisplayName)
}

func TestPublicTitleAndNoteLengthBoundaries(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Settlement{}))
	group := seedPublicShopProduct(t, db, "296", 10)
	name := strings.Repeat("编", 20)
	note := strings.Repeat("说明", 150)
	require.NoError(t, applyPublicPresentation(&group, &name, &note, nil))
	require.Equal(t, "296", group.ChannelID)
	require.Equal(t, note, group.Description)
	_, err := UpdateOwnerShop(10, ShopProfileRequest{Name: name, Description: note})
	require.NoError(t, err)
	longName := name + "程"
	require.Error(t, applyPublicPresentation(&group, &longName, nil, nil))
	_, err = UpdateOwnerShop(10, ShopProfileRequest{Name: longName, Description: note})
	require.Error(t, err)
	longNote := note + "字"
	require.Error(t, applyPublicPresentation(&group, nil, &longNote, nil))
	_, err = UpdateOwnerShop(10, ShopProfileRequest{Name: name, Description: longNote})
	require.Error(t, err)
	shop, err := GetOwnerShop(10)
	require.NoError(t, err)
	require.Equal(t, name, shop.CustomName, "invalid writes must preserve the saved title")
	require.Equal(t, note, shop.Description)
	multiline := "提供编程模型\r\n支持长上下文"
	shop, err = UpdateOwnerShop(10, ShopProfileRequest{Name: name, Description: multiline})
	require.NoError(t, err)
	require.Equal(t, "提供编程模型\n支持长上下文", shop.Description)
	require.Error(t, applyPublicPresentation(&group, &multiline, nil, nil), "titles must remain a single line")
}

func TestConnectivityRequestAlsoModeratesAndRejectsStaleResults(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}))
	group := seedPublicShopProduct(t, db, "probe-product", 10)
	channel := &marketplaceschema.Channel{ID: group.ChannelID}
	ctx, err := preparePresentationProbe(context.Background(), channel)
	require.NoError(t, err)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		require.Contains(t, string(encoded), "推理渠道")
		require.Contains(t, string(encoded), "不可信")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SAFE"}}]}`))
	}))
	defer server.Close()
	_, err = probeMarketplaceInferenceTimedContext(ctx, "openai_compatible", server.URL, "test-only", "gpt-5")
	require.NoError(t, err)
	require.Equal(t, 1, calls, "moderation must reuse the connectivity inference")
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "passed", group.ModerationStatus)
	require.Error(t, validatePresentationProbeResponse(ctx, "openai_compatible", "gpt-5", []byte(`{"choices":[{"message":{"content":"AD"}}]}`)))
	for _, text := range []string{"SAFE", "unknown"} {
		_ = validatePresentationProbeResponse(ctx, "openai_compatible", "gpt-5", []byte(`{"choices":[{"message":{"content":"`+text+`"}}]}`))
	}
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "failed", group.ModerationStatus)
	require.NoError(t, db.Model(&group).Updates(map[string]any{"custom_name": "修改后的名称", "moderation_status": "pending"}).Error)
	probe := ctx.Value(presentationProbeKey{}).(*presentationProbe)
	require.NoError(t, probe.record(context.Background(), "failed", "旧结果"))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "pending", group.ModerationStatus)
}

func TestReportsKeepEvidenceAndAdminRemovesContent(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Settlement{}))
	group := seedPublicShopProduct(t, db, "reported-product", 10)
	req := MarketReportRequest{TargetType: "channel", TargetID: group.ChannelID, Reason: "advertising", Details: "疑似变相引流"}
	require.Error(t, SubmitMarketReport(0, req))
	require.NoError(t, SubmitMarketReport(20, req))
	require.Error(t, SubmitMarketReport(20, req))
	list, err := ListMarketReports("pending", 1, 20)
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Contains(t, list.Items[0].Evidence, group.CustomName)
	require.Error(t, ResolveMarketReport(1, list.Items[0].ID, "remove_content", ""))
	require.NoError(t, ResolveMarketReport(1, list.Items[0].ID, "remove_content", "已核实并清除"))
	require.Error(t, ResolveMarketReport(1, list.Items[0].ID, "dismiss", "不能重复处理"))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Empty(t, group.CustomName)
	require.Equal(t, "[]", group.Tags)
	req.TargetID = "missing"
	require.Error(t, SubmitMarketReport(20, req))
	req.TargetID = group.ChannelID
	req.Reason = strings.Repeat("x", 40)
	require.Error(t, SubmitMarketReport(21, req))
}

func TestUserConnectivityTestUsesPresentationPrompt(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}))
	group := seedPublicShopProduct(t, db, "batch-presentation", 10)
	prompt, validate, err := batchPresentationProbe(group.ChannelID, "gpt-5")
	require.NoError(t, err)
	require.Contains(t, prompt, group.CustomName)
	require.NotNil(t, validate)
	require.NoError(t, validate([]byte(`{"choices":[{"message":{"content":"SAFE"}}]}`)))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "passed", group.ModerationStatus)
	prompt, validate, err = batchPresentationProbe("", "gpt-5")
	require.NoError(t, err)
	require.Empty(t, prompt)
	require.Nil(t, validate)
}

func TestPresentationProbeHandlesNonTextAndShopEdits(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Settlement{}))
	group := seedPublicShopProduct(t, db, "probe-boundaries", 10)
	_, err := UpdateOwnerShop(10, ShopProfileRequest{Name: "原店铺"})
	require.NoError(t, err)
	ctx, err := preparePresentationProbe(context.Background(), &marketplaceschema.Channel{ID: group.ChannelID})
	require.NoError(t, err)
	_, err = UpdateOwnerShop(10, ShopProfileRequest{Name: "新店铺"})
	require.NoError(t, err)
	require.Error(t, validatePresentationProbeResponse(ctx, "codex", "gpt-5", []byte(`{"output_text":"AD"}`)))
	shop, err := GetOwnerShop(10)
	require.NoError(t, err)
	require.Equal(t, "pending", shop.ModerationStatus, "an old probe must not moderate edited shop content")
	for _, model := range []string{"text-embedding-3-small", "bge-m3", "gpt-image-1", "gpt-5-openai-compact"} {
		prompt, validate, err := batchPresentationProbe(group.ChannelID, model)
		require.NoError(t, err)
		require.Empty(t, prompt, model)
		require.NoError(t, validate([]byte(`{"data":[]}`)), model)
	}
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "rule_passed", group.ModerationStatus)
	prompt, validate, err := batchPresentationProbe(group.ChannelID, "gpt-5")
	require.NoError(t, err)
	require.NotEmpty(t, prompt)
	require.NoError(t, validate([]byte(`{"choices":[{"message":{"content":"maybe"}}]}`)))
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "pending", group.ModerationStatus)
}

func TestChannelRenamePreservesOriginalNumericID(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.VerificationRun{}))
	group := seedPublicShopProduct(t, db, "702", 10)
	require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", "702").Update("submitted_source_label", "国产模型").Error)
	name := "自定义编程渠道"
	view, err := UpdateOwnerChannel(10, "702", UpdateChannelRequest{CustomName: &name})
	require.NoError(t, err)
	require.Equal(t, "702", view.ID)
	require.Equal(t, group.ID, view.GroupID)
	require.Equal(t, group.PublicSlug, view.PublicSlug)
	require.Equal(t, "702-国产模型-0.5x", view.SystemDisplayName)
	require.NoError(t, db.First(&group, "channel_id = ?", "702").Error)
	require.Equal(t, name, group.CustomName)
	var channel marketplaceschema.Channel
	require.NoError(t, db.First(&channel, "id = ?", "702").Error)
	require.True(t, matchesGroupQuery(group, channel, []string{"gpt-5"}, GroupQuery{Search: "702"}))
}
