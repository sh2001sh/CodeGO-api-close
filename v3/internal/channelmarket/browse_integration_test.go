//go:build pgintegration

package channelmarket_test

import (
	"encoding/json"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestBrowsePagesFilterBeforeCountingAndPreserveAccess(t *testing.T) {
	f := setup(t)
	first, second, private := f.channel(t, "public"), f.channel(t, "public"), f.channel(t, "private")
	for _, c := range []channelmarket.ChannelView{first, second, private} {
		f.active(t, c)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET display_name=CASE WHEN id=$1 THEN 'Alpha' ELSE 'Zulu' END,multiplier_ppm=CASE WHEN id=$1 THEN 2000000 ELSE 1000000 END WHERE id=ANY($2::text[])`, first.GroupID, []string{first.GroupID, second.GroupID}); err != nil {
		t.Fatal(err)
	}
	o := channelmarket.BrowseOptions{Page: 1, PageSize: 1, Scope: "all", Sort: "multiplier", PriceBasis: "input"}
	p, err := f.s.BrowseGroups(ctx, channelmarket.Actor{}, o)
	if err != nil || p.Pagination.Total != 2 || len(p.Groups) != 1 || p.Groups[0].ID != second.ID {
		t.Fatalf("first page %+v %v", p, err)
	}
	o.Page = 2
	p, err = f.s.BrowseGroups(ctx, channelmarket.Actor{}, o)
	if err != nil || len(p.Groups) != 1 || p.Groups[0].ID != first.ID {
		t.Fatalf("second page %+v %v", p, err)
	}
	o.Page = 1
	o.Search = "Alpha"
	p, err = f.s.BrowseGroups(ctx, channelmarket.Actor{}, o)
	if err != nil || p.Pagination.Total != 1 || p.Groups[0].ID != first.ID {
		t.Fatalf("cross-page search %+v %v", p, err)
	}
	o.Search = "%"
	p, err = f.s.BrowseGroups(ctx, channelmarket.Actor{}, o)
	if err != nil || p.Pagination.Total != 0 {
		t.Fatalf("SQL wildcard interpreted as search-all %+v %v", p, err)
	}
	o.Search = ""
	o.Page = 10
	p, err = f.s.BrowseGroups(ctx, channelmarket.Actor{}, o)
	if err != nil || len(p.Groups) != 0 || p.Pagination.Total != 2 {
		t.Fatalf("out of range page %+v %v", p, err)
	}
	o.Page = 1
	p, err = f.s.BrowseGroups(ctx, channelmarket.Actor{UserID: 1}, o)
	if err != nil || p.Pagination.Total != 3 {
		t.Fatalf("owner private access %+v %v", p, err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.channel_user_blocks(channel_id,user_id) VALUES($1,2)`, second.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	p, err = f.s.BrowseGroups(ctx, channelmarket.Actor{UserID: 2}, o)
	if err != nil || p.Pagination.Total != 1 || p.Groups[0].ID != first.ID {
		t.Fatalf("block/private leak %+v %v", p, err)
	}
	shop, err := f.s.MyShop(ctx, channelmarket.Actor{UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	shops, err := f.s.BrowseShops(ctx, channelmarket.Actor{}, o)
	if err != nil || shops.Pagination.Total != 1 || len(shops.Shops) != 1 {
		t.Fatalf("shops %+v %v", shops, err)
	}
	o.Search = "Zulu"
	detail, err := f.s.BrowseShop(ctx, channelmarket.Actor{}, shop.ID, o)
	if err != nil || detail.Pagination.Total != 1 || detail.Shop.GroupCount != 2 || len(detail.Groups) != 1 || detail.Groups[0].ID != second.ID {
		t.Fatalf("shop filter/global summary %+v %v", detail, err)
	}
}

func TestPublicCatalogIsCompleteBeyondBrowserPageAndQuotesStayExact(t *testing.T) {
	f := setup(t)
	base := f.channel(t, "public")
	f.active(t, base)
	// Clone 1,005 live groups directly: the regression is the former SQL
	// LIMIT, not channel submission or verification behavior.
	for _, query := range []string{
		`INSERT INTO v3_catalog.groups(name) SELECT 'market_browse_'||i FROM generate_series(1,1005) i`,
		`INSERT INTO v3_catalog.channels(name,provider,base_url,status,scope,owner_user_id) SELECT 'browse-channel-'||i,'openai','https://example.test','enabled','marketplace',1 FROM generate_series(1,1005) i`,
		`INSERT INTO v3_catalog.channel_models(channel_id,model) SELECT id,'fixture-model' FROM v3_catalog.channels WHERE name LIKE 'browse-channel-%'`,
		`INSERT INTO v3_channelmarket.groups(id,channel_id,owner_user_id,public_slug,internal_group_name,display_name,source_label,multiplier_ppm,visibility,lifecycle_status,verification_status,model_prices,public_channel_id)
 SELECT 'browse-'||i,c.id,1,'browse-slug-'||i,'market_browse_'||i,'Group '||i,'',1000000,'public','active','passed','{}', (10000+i)::text FROM generate_series(1,1005) i JOIN v3_catalog.channels c ON c.name='browse-channel-'||i`,
	} {
		if _, err := f.pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	models, err := f.s.PublicModels(ctx, channelmarket.Actor{})
	if err != nil || len(models) != 1 || len(models[0].Groups) != 1006 {
		t.Fatalf("catalog omitted groups beyond1000: models=%d %v", len(models), err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET display_name='Baseline exact',model_prices=$2 WHERE id=$1`, base.GroupID, json.RawMessage(`{"fixture-model":{"billing_mode":"token","input_price_per_million":1.234567,"output_price_per_million":2}}`)); err != nil {
		t.Fatal(err)
	}
	o := channelmarket.BrowseOptions{Page: 1, PageSize: 10, Search: "Baseline exact", Model: "fixture-model", Scope: "all", Sort: "name", PriceBasis: "input"}
	page, err := f.s.BrowseGroups(ctx, channelmarket.Actor{}, o)
	if err != nil || len(page.Groups) != 1 {
		t.Fatalf("page quote %+v %v", page, err)
	}
	if quote := page.Groups[0].EffectivePrices["fixture-model"]; quote == nil || quote.InputPerMillion != "0.1234567" {
		t.Fatalf("multiplier quote %+v", quote)
	}
	o.Model = ""
	page, err = f.s.BrowseGroups(ctx, channelmarket.Actor{}, o)
	if err != nil || len(page.Groups[0].EffectivePrices) != 0 {
		t.Fatalf("summary shipped all-model price matrix %+v %v", page, err)
	}
	full, err := f.s.Get(ctx, channelmarket.Actor{}, base.ID)
	if err != nil || len(full.EffectivePrices) != 1 {
		t.Fatalf("detail quote lost %+v %v", full, err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES($1,'unquoted-model')`, base.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO v3_catalog.model_prices(model,mode,input_per_mtok,output_per_mtok) VALUES('unquoted-model','per_token',9000000,9000000)`); err != nil {
		t.Fatal(err)
	}
	o.Model = "unquoted-model"
	page, err = f.s.BrowseGroups(ctx, channelmarket.Actor{}, o)
	if err != nil || len(page.Groups) != 1 || page.Groups[0].EffectivePrices["unquoted-model"] != nil {
		t.Fatalf("missing seller quote incorrectly inherited global price: %+v %v", page, err)
	}
}
