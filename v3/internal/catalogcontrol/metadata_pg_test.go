//go:build pgintegration

package catalogcontrol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestMetadataControlPostgres(t *testing.T) {
	f := newMetadataFixture(t)
	for _, path := range []string{"/api/models/", "/api/vendors/", "/api/prefill_group/", "/api/catalog/prices"} {
		w := httptest.NewRecorder()
		f.mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 403 {
			t.Fatalf("metadata route lacks required administrator wrapper %s: %d", path, w.Code)
		}
	}
	closed := http.NewServeMux()
	f.server.Register(closed, nil)
	w := httptest.NewRecorder()
	closed.ServeHTTP(w, httptest.NewRequest("GET", "/api/catalog/prices", nil))
	if w.Code != 403 {
		t.Fatal("missing admin middleware did not fail closed")
	}
	f.call(t, "POST", "/api/vendors/", `{"name":"retained-vendor","icon":"source-icon","status":1}`, 200)
	f.call(t, "POST", "/api/catalog/vendors", `{"name":"retained-vendor"}`, 409)
	f.call(t, "PUT", "/api/vendors/", `{"id":1,"name":"retained-vendor","description":"updated source vendor","status":1}`, 200)
	f.call(t, "GET", "/api/vendors/search?keyword=updated&page_size=1", "", 200)
	f.call(t, "POST", "/api/models/", `{"model_name":"gpt-","name_rule":1,"vendor_id":1,"status":1,"sync_official":0,"tags":"general","endpoints":"[\"openai\"]"}`, 200)
	f.call(t, "POST", "/api/models/", `{"model_name":"gpt-","name_rule":1}`, 409)
	f.call(t, "POST", "/api/models/", `{"model_name":"broken","vendor_id":999}`, 409)
	f.call(t, "POST", "/api/models/", `{"model_name":"broken","name_rule":-1}`, 400)
	f.call(t, "PUT", "/api/models/?status_only=true", `{"id":1,"status":0}`, 200)
	f.call(t, "PUT", "/api/catalog/models/1?status_only=true", `{"status":1}`, 200)
	f.call(t, "GET", "/api/models/search?status=enabled&sync_official=no&vendor=1&keyword=gpt", "", 200)
	metadataExec(t, f.pool, `INSERT INTO v3_catalog.groups(name) VALUES('default');
		INSERT INTO v3_catalog.channels(id,name,provider) VALUES(1,'metadata-channel','openai');
		INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(1,'default');
		INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(1,'gpt-4'),(1,'no-metadata');
		INSERT INTO v3_catalog.model_prices(model,input_per_mtok) VALUES('gpt-4',9007199254740993),('no-metadata',3)`)
	w = f.call(t, "GET", "/api/models/1", "", 200)
	var result struct {
		Data metadataModelView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.MatchedCount != 1 || result.Data.MatchedModels[0] != "gpt-4" || len(result.Data.BoundChannels) != 1 || result.Data.BoundChannels[0].Name != "metadata-channel" || len(result.Data.EnableGroups) != 1 || result.Data.EnableGroups[0] != "default" {
		t.Fatalf("model rule not consumed by actual model admin discovery: %+v", result.Data)
	}
	w = f.call(t, "GET", "/api/models/missing", "", 200)
	if !strings.Contains(w.Body.String(), `"no-metadata"`) || !strings.Contains(w.Body.String(), `"gpt-4"`) {
		t.Fatalf("missing exact model metadata differs from source: %s", w.Body.String())
	}
	w = f.call(t, "GET", "/api/catalog/prices", "", 200)
	var prices struct {
		Data []metadataPriceView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &prices); err != nil {
		t.Fatal(err)
	}
	if len(prices.Data) != 2 || prices.Data[0].InputPerMTok != 9007199254740993 || prices.Data[0].Metadata == nil || prices.Data[0].Metadata.NameRule != catalog.NameRulePrefix || prices.Data[0].Vendor == nil || prices.Data[0].Vendor.Name != "retained-vendor" || prices.Data[1].Metadata != nil {
		t.Fatalf("native price or matching model/vendor metadata not consumed: %+v", prices.Data)
	}
	f.call(t, "POST", "/api/prefill_group/", `{"name":"source-models","type":"model","items":["gpt-4","claude"]}`, 200)
	f.call(t, "POST", "/api/prefill_group/", `{"name":"source-models","type":"tag","items":[]}`, 409)
	f.call(t, "POST", "/api/catalog/prefill-groups", `{"name":"source-endpoints","type":"endpoint","items":{"openai":{"path":"/v1/chat/completions"}}}`, 200)
	f.call(t, "PUT", "/api/prefill_group/", `{"id":1,"name":"source-models","type":"model","items":["gpt-4"]}`, 200)
	w = f.call(t, "GET", "/api/prefill_group/?type=model", "", 200)
	if !strings.Contains(w.Body.String(), `"gpt-4"`) || strings.Contains(w.Body.String(), "source-endpoints") {
		t.Fatal("prefill list filter or items changed")
	}
	f.call(t, "POST", "/api/prefill_group/", `{"name":"bad","type":"tag","items":["  "]}`, 400)
	f.call(t, "DELETE", "/api/models/1", "", 200)
	f.call(t, "GET", "/api/models/1", "", 404)
	f.call(t, "PUT", "/api/models/", `{"id":1,"model_name":"gpt-"}`, 404)
	f.call(t, "DELETE", "/api/models/1", "", 404)
	id := metadataResultID(t, f.call(t, "POST", "/api/models/", `{"model_name":"gpt-","name_rule":1,"vendor_id":1}`, 200))
	if id == 1 {
		t.Fatal("deleted source model identity was reused")
	}
	f.call(t, "DELETE", "/api/vendors/1", "", 200)
	f.call(t, "GET", "/api/vendors/1", "", 404)
	f.call(t, "DELETE", "/api/prefill_group/1", "", 200)
	data, err := catalog.ReadMetadata(context.Background(), f.pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Vendors) != 0 || len(data.Models) != 1 || len(data.PrefillGroups) != 1 || data.Models[0].ID != id {
		t.Fatalf("deleted records entered runtime snapshot: %+v", data)
	}
	snapshot, err := catalog.Compile(context.Background(), f.pool, f.cipher)
	if err != nil {
		t.Fatal(err)
	}
	matched, vendor := snapshot.Metadata.Describe("gpt-4")
	if matched == nil || matched.ID != id || vendor != nil || len(snapshot.Metadata.PrefillGroups) != 1 {
		t.Fatal("PG Compile did not publish usable active metadata")
	}
	var deleted, invalidations int
	if err = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM v3_catalog.models WHERE deleted_at IS NOT NULL`).Scan(&deleted); err != nil || deleted != 1 {
		t.Fatalf("soft delete history lost: %d %v", deleted, err)
	}
	if err = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity='catalog'`).Scan(&invalidations); err != nil || invalidations < 12 {
		t.Fatalf("metadata write invalidations missing: %d %v", invalidations, err)
	}
}
