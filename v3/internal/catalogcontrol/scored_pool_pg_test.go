//go:build pgintegration

package catalogcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScoredPoolPostgresHTTPExactCostsAndAtomicReplacement(t *testing.T) {
	ctx := context.Background()
	db := testPool(t)
	if _, err := db.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES('default'); INSERT INTO v3_catalog.channels(id,name,provider) VALUES(1,'channel-a','openai'),(2,'channel-b','openai')`); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, nil)
	mux := http.NewServeMux()
	s.Register(mux, func(next http.Handler) http.Handler { return next })
	call := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != status {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	native := `{"group":"default","model":"*","strategy":"weighted","enabled":true,"members":[{"channel_id":1,"weight":1}]}`
	call("PUT", "/api/catalog/route-pools", native, 200)
	call("PUT", "/api/catalog/route-pools", native, 200)
	if _, err := db.Exec(ctx, `INSERT INTO v3_catalog.route_pools(id,name,group_name,model,strategy,enabled,deleted_at) VALUES(100,'disabled','default','*','scored',false,NULL),(101,'deleted','default','*','scored',true,now())`); err != nil {
		t.Fatal(err)
	}
	w := call("POST", "/api/route-pools/", `{"name":"active source","group":"default","enabled":true,"auto_discover":true,"model_scope":"","ttft_weight":80,"members":[{"id":201,"route_pool_id":0,"channel_id":1,"cost_multiplier":1.12345678901234567890,"model_cost_overrides":"{\"gpt-4\":0.000000000000000003}","fault_domain":"source-domain","enabled":false}]}`, 200)
	var result struct {
		Data struct{ ID int64 } `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data.ID == 0 {
		t.Fatalf("save response: %s %v", w.Body.String(), err)
	}
	id := result.Data.ID
	var cost, override, domain string
	var enabled bool
	var legacyID int64
	if err := db.QueryRow(ctx, `SELECT cost_multiplier::text,model_cost_overrides->>'gpt-4',fault_domain,enabled,legacy_id FROM v3_catalog.route_pool_members WHERE pool_id=$1`, id).Scan(&cost, &override, &domain, &enabled, &legacyID); err != nil || cost != "1.12345678901234567890" || override != "0.000000000000000003" || domain != "source-domain" || enabled || legacyID != 201 {
		t.Fatalf("source member changed: %s %s %s %t %d %v", cost, override, domain, enabled, legacyID, err)
	}
	call("PUT", "/api/catalog/route-pools", `{"name":"invalid replacement","group":"default","strategy":"scored","enabled":true,"members":[{"channel_id":999,"weight":1}]}`, 409)
	if err := db.QueryRow(ctx, `SELECT enabled FROM v3_catalog.route_pools WHERE id=$1`, id).Scan(&enabled); err != nil || !enabled {
		t.Fatalf("failed replacement disabled original: %t %v", enabled, err)
	}
	call("PUT", "/api/catalog/route-pools", fmt.Sprintf(`{"id":%d,"name":"active source","group":"default","strategy":"scored","enabled":true,"members":[{"channel_id":1,"weight":1,"cost_multiplier":0.5}]}`, id), 200)
	if err := db.QueryRow(ctx, `SELECT legacy_id FROM v3_catalog.route_pool_members WHERE pool_id=$1`, id).Scan(&legacyID); err != nil || legacyID != 201 {
		t.Fatalf("edit without legacy ID rewrote source identity: %d %v", legacyID, err)
	}
	call("PUT", "/api/catalog/route-pools", `{"name":"next active","group":"default","strategy":"scored","enabled":true,"members":[{"channel_id":2,"weight":1,"cost_multiplier":0.75}]}`, 200)
	if err := db.QueryRow(ctx, `SELECT enabled FROM v3_catalog.route_pools WHERE id=$1`, id).Scan(&enabled); err != nil || enabled {
		t.Fatalf("new active did not disable sibling: %t %v", enabled, err)
	}
	call("PUT", "/api/catalog/route-pools", `{"name":"bad cost","group":"default","strategy":"scored","enabled":true,"members":[{"channel_id":2,"weight":1,"cost_multiplier":-1}]}`, 400)
	w = call("GET", "/api/catalog/route-pools", "", 200)
	if !strings.Contains(w.Body.String(), "disabled") || !strings.Contains(w.Body.String(), "deleted") || !strings.Contains(w.Body.String(), `"cost_multiplier":0.75`) {
		t.Fatalf("history or numeric DTO omitted: %s", w.Body.String())
	}
	protected := http.NewServeMux()
	s.Register(protected, nil)
	w = httptest.NewRecorder()
	protected.ServeHTTP(w, httptest.NewRequest("PUT", "/api/catalog/route-pools", strings.NewReader(native)))
	if w.Code != 403 {
		t.Fatalf("unauthenticated pool mutation: %d", w.Code)
	}
}
