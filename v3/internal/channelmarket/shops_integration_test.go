//go:build pgintegration

package channelmarket_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/community"
)

func TestShopProviderTagsUseJSONValues(t *testing.T) {
	f := setup(t)
	first, second := f.channel(t, "public"), f.channel(t, "public")
	f.active(t, first)
	f.active(t, second)
	// The catalog's unrelated tag column must not shadow lateral JSON values.
	if _, err := f.pool.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market,tags}','["openai","anthropic"]'),tag=NULL WHERE id=$1`, first.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market,tags}','["openai"]'),tag='internal-channel-label' WHERE id=$1`, second.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []channelmarket.Actor{{}, {UserID: 3, Admin: true}} {
		shops, err := f.s.ListShops(ctx, actor, actor.Admin)
		if err != nil || len(shops) != 1 {
			t.Fatalf("shop listing with provider tags: %+v %v", shops, err)
		}
		if !reflect.DeepEqual(shops[0].Tags, []string{"anthropic", "openai"}) {
			t.Fatalf("provider tags shadowed by catalog tag: %v", shops[0].Tags)
		}
	}
}

func TestShopReviewPreservesPublishedTextAndDoesNotExposePrivateGroups(t *testing.T) {
	f := setup(t)
	public := f.channel(t, "public")
	f.active(t, public)
	private := f.channel(t, "private")
	f.active(t, private)
	owner := channelmarket.Actor{UserID: 1}
	admin := channelmarket.Actor{UserID: 3, Admin: true}
	mine, err := f.s.MyShop(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if mine.ID == "1" || mine.ID == "" {
		t.Fatalf("shop identity reused private owner ID: %+v", mine)
	}
	before, err := f.s.GetShop(ctx, channelmarket.Actor{}, mine.ID)
	if err != nil || len(before.Groups) != 1 || before.Groups[0].ID != public.ID || before.Shop.GroupCount != 1 {
		t.Fatalf("public/private scope mismatch: %+v %v", before, err)
	}
	if before.Groups[0].Shop == nil || before.Groups[0].Shop.ID != mine.ID {
		t.Fatalf("missing shop reference: %+v", before.Groups[0])
	}
	staged, err := f.s.UpdateShop(ctx, owner, channelmarket.ShopUpdate{Name: "海港模型研究", Description: "提供文本模型调用。"})
	if err != nil || staged.ReviewStatus != "pending" || staged.Name != mine.Name {
		t.Fatalf("profile did not stage: %+v %v", staged, err)
	}
	hidden, err := f.s.GetShop(ctx, channelmarket.Actor{}, mine.ID)
	if err != nil || hidden.Shop.Name != mine.Name || hidden.Shop.SubmittedName != "" || hidden.Shop.ReviewStatus != "" || hidden.Shop.SubmittedDescription != nil {
		t.Fatalf("pending text leaked publicly: %+v %v", hidden, err)
	}
	if err = f.s.ReviewShop(ctx, channelmarket.Actor{UserID: 2}, mine.ID, true, ""); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("foreign actor reviewed shop: %v", err)
	}
	if _, err = f.s.MyShop(ctx, channelmarket.Actor{UserID: 2}); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("nonowner fabricated shop: %v", err)
	}
	if err = f.s.ReviewShop(ctx, admin, mine.ID, false, "请调整描述"); err != nil {
		t.Fatal(err)
	}
	rejected, err := f.s.GetShop(ctx, channelmarket.Actor{}, mine.ID)
	if err != nil || rejected.Shop.Name != mine.Name {
		t.Fatalf("rejection changed published text: %+v %v", rejected, err)
	}
	if _, err = f.s.UpdateShop(ctx, owner, channelmarket.ShopUpdate{Name: "海港模型研究", Description: "提供文本模型调用。"}); err != nil {
		t.Fatal(err)
	}
	if err = f.s.ReviewShop(ctx, admin, mine.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	approved, err := f.s.GetShop(ctx, channelmarket.Actor{}, mine.ID)
	if err != nil || approved.Shop.Name != "海港模型研究" || approved.Shop.ID != mine.ID || approved.Groups[0].GroupID != public.GroupID {
		t.Fatalf("approval changed identity: %+v %v", approved, err)
	}
	list, err := f.s.ListShops(ctx, channelmarket.Actor{}, false)
	if err != nil || len(list) != 1 || list[0].GroupCount != 1 || len(list[0].Models) != 1 {
		t.Fatalf("shop summary wrong: %+v %v", list, err)
	}
	if _, err = f.s.UpdateShop(ctx, owner, channelmarket.ShopUpdate{Name: "微信１２３４５６７８"}); !errors.Is(err, channelmarket.ErrInvalidName) {
		t.Fatalf("contact advertisement accepted: %v", err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE v3_identity.users SET status='disabled' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.GetShop(ctx, channelmarket.Actor{}, mine.ID); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("disabled seller still public: %v", err)
	}
}

func TestShopRatingsDeduplicateConsumersAndMatchCommunity(t *testing.T) {
	f := setup(t)
	first, second, private := f.channel(t, "public"), f.channel(t, "public"), f.channel(t, "private")
	for _, g := range []channelmarket.ChannelView{first, second, private} {
		f.active(t, g)
	}
	_, err := f.pool.Exec(ctx, `UPDATE v3_identity.users SET external_id='ABC234' WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO v3_community.channel_ratings(channel_id,user_id,stars) VALUES($1,2,4),($1,3,2),($2,2,5),($3,2,1)`, first.ID, second.ID, private.ID)
	if err != nil {
		t.Fatal(err)
	}
	shops, err := f.s.ListShops(ctx, channelmarket.Actor{}, false)
	if err != nil || len(shops) != 1 || shops[0].Rating.AverageScore != 6.5 || shops[0].Rating.RatingCount != 2 {
		t.Fatalf("shop ratings %+v %v", shops, err)
	}
	detail, err := f.s.GetShop(ctx, channelmarket.Actor{}, shops[0].ID)
	if err != nil || detail.Shop.Rating != shops[0].Rating {
		t.Fatalf("detail summary %+v %v", detail, err)
	}
	groups, err := f.s.List(ctx, channelmarket.Actor{}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.ID == first.ID && (g.Rating.AverageScore != 6 || g.Rating.RatingCount != 2) {
			t.Fatalf("group rating %+v", g.Rating)
		}
	}
	member, err := community.New(f.pool, community.Config{}).GetMember(ctx, "ABC234")
	if err != nil || member.AverageScore != 6.5 || member.RatingCount != 2 || member.DisplayName != shops[0].Name {
		t.Fatalf("community mismatch %+v %v", member, err)
	}
}

func TestShopBlocksAndResetKeepStableIdentity(t *testing.T) {
	f := setup(t)
	public := f.channel(t, "public")
	f.active(t, public)
	owner := channelmarket.Actor{UserID: 1}
	admin := channelmarket.Actor{UserID: 3, Admin: true}
	mine, err := f.s.MyShop(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.UpdateShop(ctx, owner, channelmarket.ShopUpdate{Name: "Harbour AI", Description: "Text models."}); err != nil {
		t.Fatal(err)
	}
	if err = f.s.ReviewShop(ctx, admin, mine.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.UpdateShop(ctx, owner, channelmarket.ShopUpdate{}); err != nil {
		t.Fatal(err)
	}
	pending, err := f.s.GetShop(ctx, channelmarket.Actor{}, mine.ID)
	if err != nil || pending.Shop.Name != "Harbour AI" {
		t.Fatalf("blank reset bypassed review: %+v %v", pending, err)
	}
	if err = f.s.ReviewShop(ctx, admin, mine.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	reset, err := f.s.GetShop(ctx, channelmarket.Actor{}, mine.ID)
	if err != nil || reset.Shop.Name != mine.Name || reset.Shop.ID != mine.ID {
		t.Fatalf("reset changed identity: %+v %v", reset, err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.channel_user_blocks(channel_id,user_id) VALUES($1,2)`, public.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.GetShop(ctx, channelmarket.Actor{UserID: 2}, mine.ID); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("blocked viewer accessed shop group: %v", err)
	}
	list, err := f.s.ListShops(ctx, channelmarket.Actor{UserID: 2}, false)
	if err != nil || len(list) != 0 {
		t.Fatalf("blocked shop in directory: %+v %v", list, err)
	}
}
