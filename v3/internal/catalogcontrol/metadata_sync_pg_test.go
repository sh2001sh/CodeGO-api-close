//go:build pgintegration

package catalogcontrol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetadataSyncPreviewAndSelectiveOverwritePostgres(t *testing.T) {
	f := newMetadataFixture(t)
	f.call(t, "POST", "/api/vendors", `{"name":"local-vendor","status":1}`, 200)
	f.call(t, "POST", "/api/models", `{"model_name":"alpha","description":"local","icon":"keep-local-icon","sync_official":1,"vendor_id":1}`, 200)
	f.call(t, "POST", "/api/models", `{"model_name":"gamma","description":"user-owned","sync_official":0}`, 200)
	metadataExec(t, f.pool, `INSERT INTO v3_catalog.channels(id,name,provider) VALUES(1,'sync-channel','openai');
		INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(1,'alpha'),(1,'beta'),(1,'gamma'),(1,'unknown')`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models.json") {
			_, _ = w.Write([]byte(`{"success":true,"data":[
				{"model_name":"alpha","description":"source-alpha","icon":"source-icon","vendor_name":"source-vendor"},
				{"model_name":"beta","description":"source-beta","vendor_name":"source-vendor"},
				{"model_name":"gamma","description":"must-not-overwrite","vendor_name":"source-vendor"}]}`))
		} else {
			_, _ = w.Write([]byte(`[{"name":"source-vendor","description":"source-description","status":1}]`))
		}
	}))
	defer upstream.Close()
	t.Setenv("SYNC_UPSTREAM_BASE", upstream.URL)
	w := f.call(t, "GET", "/api/models/sync_upstream/preview?locale=zh-cn", "", 200)
	var preview struct {
		Data struct {
			Missing   []string           `json:"missing"`
			Conflicts []metadataConflict `json:"conflicts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil || len(preview.Data.Missing) != 1 || preview.Data.Missing[0] != "beta" || len(preview.Data.Conflicts) != 1 || preview.Data.Conflicts[0].ModelName != "alpha" {
		t.Fatalf("preview missing/conflict/sync-official policy: %s %v", w.Body.String(), err)
	}
	w = f.call(t, "POST", "/api/models/sync_upstream", `{"locale":"zh-cn","overwrite":[
		{"model_name":"alpha","fields":["description","vendor"]},
		{"model_name":"gamma","fields":["description"]}]}`, 200)
	var result struct {
		Data metadataSyncResult `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data.CreatedModels != 1 || result.Data.CreatedVendors != 1 || result.Data.UpdatedModels != 1 || len(result.Data.SkippedModels) != 1 || result.Data.SkippedModels[0] != "unknown" {
		t.Fatalf("sync counts: %s %v", w.Body.String(), err)
	}
	var description, icon, vendor string
	if err := f.pool.QueryRow(context.Background(), `SELECT m.description,m.icon,v.name FROM v3_catalog.models m
		JOIN v3_catalog.vendors v ON v.id=m.vendor_id WHERE m.model_name='alpha'`).Scan(&description, &icon, &vendor); err != nil || description != "source-alpha" || icon != "keep-local-icon" || vendor != "source-vendor" {
		t.Fatalf("selected overwrite not honored: %s %s %s %v", description, icon, vendor, err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT description FROM v3_catalog.models WHERE model_name='gamma'`).Scan(&description); err != nil || description != "user-owned" {
		t.Fatal("sync_official=false model overwritten")
	}
	f.call(t, "POST", "/api/models/sync_upstream", `{"overwrite":[{"model_name":"alpha","fields":["unrecognized"]}]}`, 400)
	// The source's prefill calls omit trailing '/', including POST and PUT.
	f.call(t, "POST", "/api/prefill_group", `{"name":"no-slash","type":"tag","items":["source"]}`, 200)
	f.call(t, "PUT", "/api/prefill_group", `{"id":1,"name":"no-slash","type":"tag","items":[]}`, 200)
	f.call(t, "GET", "/api/prefill_group?type=tag", "", 200)
	w = f.call(t, "POST", "/api/models/sync_upstream", `{}`, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data.CreatedModels != 0 || result.Data.CreatedVendors != 0 {
		t.Fatal("second sync created duplicate metadata")
	}
}

func TestMetadataSyncFailureAndTransactionRollbackPostgres(t *testing.T) {
	f := newMetadataFixture(t)
	metadataExec(t, f.pool, `INSERT INTO v3_catalog.channels(id,name,provider) VALUES(1,'rollback-channel','openai');
		INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(1,'a'),(1,'b');
		CREATE FUNCTION v3_catalog.metadata_reject_b() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.model_name='b' THEN RAISE EXCEPTION 'test induced failure' USING ERRCODE='23514'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER metadata_reject_b BEFORE INSERT ON v3_catalog.models FOR EACH ROW EXECUTE FUNCTION v3_catalog.metadata_reject_b()`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models.json") {
			_, _ = w.Write([]byte(`[{"model_name":"a","vendor_name":"vendor"},{"model_name":"b","vendor_name":"vendor"}]`))
		} else {
			_, _ = w.Write([]byte(`[{"name":"vendor"}]`))
		}
	}))
	defer upstream.Close()
	t.Setenv("SYNC_UPSTREAM_BASE", upstream.URL)
	f.call(t, "POST", "/api/models/sync_upstream", `{}`, 400)
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM v3_catalog.models)+(SELECT count(*) FROM v3_catalog.vendors)`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed sync partially committed model/vendor: %d %v", count, err)
	}
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("fixture-secret"))
	}))
	defer failed.Close()
	t.Setenv("SYNC_UPSTREAM_BASE", failed.URL)
	w := f.call(t, "POST", "/api/models/sync_upstream", `{}`, 502)
	if strings.Contains(w.Body.String(), "fixture-secret") {
		t.Fatal("upstream error body leaked")
	}
}
