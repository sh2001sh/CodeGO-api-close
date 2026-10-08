//go:build pgintegration

package channelmarket_test

import (
	"encoding/json"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestPublicModelCatalogPricingAndAccess(t *testing.T) {
	f := setup(t)
	public := f.channel(t, "public")
	private := f.channel(t, "private")
	f.active(t, public)
	f.active(t, private)
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_catalog.models(id,model_name) VALUES(17,'fixture-model');
		INSERT INTO v3_catalog.model_prices(model,mode,input_per_mtok,output_per_mtok) VALUES('fixture-model','per_token',2000000,8000000)`); err != nil {
		t.Fatal(err)
	}
	models, err := f.s.PublicModels(ctx, channelmarket.Actor{})
	if err != nil || len(models) != 1 || len(models[0].Groups) != 1 {
		t.Fatalf("anonymous catalog must exclude private groups: %+v, %v", models, err)
	}
	model := models[0]
	if model.ModelID == nil || *model.ModelID != 17 || model.Groups[0].Price.InputPerMillion != "0.2" || model.Groups[0].Price.OutputPerMillion != "0.8" {
		t.Fatalf("incorrect public metadata/pricing: %+v", model)
	}
	data, _ := json.Marshal(model)
	var fields map[string]any
	_ = json.Unmarshal(data, &fields)
	for _, secret := range []string{"api_key", "base_url", "owner_user_id", "internal_channel_id", "settings"} {
		if _, exists := fields[secret]; exists {
			t.Fatalf("public catalog exposes %s", secret)
		}
	}
	models, err = f.s.PublicModels(ctx, channelmarket.Actor{UserID: 1})
	if err != nil || len(models[0].Groups) != 2 {
		t.Fatalf("owner should see active private group: %+v, %v", models, err)
	}
	if err := f.s.SetBlock(ctx, channelmarket.Actor{UserID: 1}, public.InternalChannelID, 2, true); err != nil {
		t.Fatal(err)
	}
	models, err = f.s.PublicModels(ctx, channelmarket.Actor{UserID: 2})
	if err != nil || len(models) != 0 {
		t.Fatalf("blocked user must not see public quotation: %+v, %v", models, err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name,multiplier) VALUES('default',0.5),('internal-only',1);
		INSERT INTO v3_catalog.channels(id,name,provider,status,scope) VALUES(999,'official-private-source','openai','enabled','official');
		INSERT INTO v3_catalog.channel_credentials(channel_id,secret) VALUES(999,'not-a-public-secret');
		INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(999,'default'),(999,'internal-only');
		INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(999,'*')`); err != nil {
		t.Fatal(err)
	}
	models, err = f.s.PublicModels(ctx, channelmarket.Actor{})
	if err != nil || len(models) != 1 || len(models[0].Groups) != 2 {
		t.Fatalf("public market plus globally usable official group expected: %+v, %v", models, err)
	}
	for _, group := range models[0].Groups {
		if group.Slug == "internal-only" {
			t.Fatal("internal official group was disclosed")
		}
		if group.Slug == "default" && group.Price.InputPerMillion != "1" {
			t.Fatalf("official group quotation: %+v", group.Price)
		}
	}
}
