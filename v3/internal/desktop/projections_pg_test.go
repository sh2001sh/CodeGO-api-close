//go:build pgintegration

package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func TestDesktopRetainsStatusSeriesMetadataAndRecommendedConfigs(t *testing.T) {
	s, a, _ := desktopFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 8, 15, 0, 0, time.UTC)
	s.cfg.Now = func() time.Time { return now }
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES('default')`); err != nil {
		t.Fatal(err)
	}
	var cid int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO v3_catalog.channels(name,provider) VALUES('desktop-fixture','claude') RETURNING id`).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES($1,'default')`,
		`INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES($1,'claude-sonnet-4-5')`,
	} {
		if _, err := s.pool.Exec(ctx, q, cid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_catalog.model_prices(model,input_per_mtok,output_per_mtok) VALUES('claude-sonnet-4-5',3000000,15000000); INSERT INTO v3_catalog.vendors(id,name,description,icon) VALUES(101,'Anthropic','vendor description','vendor-icon'); INSERT INTO v3_catalog.models(id,model_name,description,icon,tags,endpoints,vendor_id,name_rule) VALUES(101,'claude-','model description','model-icon','coding','{"anthropic":{"path":"/v1/messages","method":"POST"}}',101,1)`); err != nil {
		t.Fatal(err)
	}
	start, err := s.Start(ctx, StartInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(ctx, a.ID, start.SessionID, true); err != nil {
		t.Fatal(err)
	}
	poll, err := s.Poll(ctx, start.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := func(path string) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+poll.AccessToken)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out["success"] != true {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return out
	}
	groups := request("/api/desktop/group-status")["data"].([]any)
	model := groups[0].(map[string]any)["models"].([]any)[0].(map[string]any)
	if len(model["series"].([]any)) != 12 {
		t.Fatalf("missing retained six-hour timeline: %v", model)
	}
	seedDesktopStatusFacts(t, s, a.ID, cid, now)
	groups = request("/api/desktop/group-status")["data"].([]any)
	model = groups[0].(map[string]any)["models"].([]any)[0].(map[string]any)
	if model["request_count"] != float64(4) || model["success_rate"] != float64(75) || model["status"] != "unstable" || model["cache_hit_rate"] != float64(50) {
		t.Fatal("latest half-hour / cache facts differ", model)
	}
	pricing := request("/api/desktop/pricing")
	if len(pricing["vendors"].([]any)) != 1 {
		t.Fatal("vendor metadata omitted", pricing)
	}
	price := pricing["data"].([]any)[0].(map[string]any)
	if price["description"] != "model description" || price["owner_by"] != "Anthropic" {
		t.Fatal("model metadata omitted", price)
	}
	if len(price["supported_endpoint_types"].([]any)) != 1 || pricing["supported_endpoint"].(map[string]any)["anthropic"].(map[string]any)["path"] != "/v1/messages" {
		t.Fatal("model endpoint metadata omitted", price)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE v3_catalog.model_prices SET mode='expression',rules='{"expression":"p * 3","image_ratio":1.5,"audio_ratio":2.5,"audio_completion_ratio":3.5}' WHERE model='claude-sonnet-4-5'`); err != nil {
		t.Fatal(err)
	}
	price = request("/api/desktop/pricing")["data"].([]any)[0].(map[string]any)
	if price["billing_mode"] != "tiered_expr" || price["billing_expr"] != "p * 3" || price["image_ratio"] != float64(1.5) || price["audio_ratio"] != float64(2.5) || price["audio_completion_ratio"] != float64(3.5) {
		t.Fatal("retained expression/media pricing omitted", price)
	}
	k, _, err := s.id.CreateKey(ctx, a.ID, identity.KeyInput{Name: "native-config"})
	if err != nil {
		t.Fatal(err)
	}
	configs := request(fmt.Sprintf("/api/desktop/tokens/%d/config", k.ID))["data"].(map[string]any)["tools"].(map[string]any)
	if configs["claude"].(map[string]any)["model"] != "claude-sonnet-4-5" {
		t.Fatal("configured unavailable fallback model", configs["claude"])
	}
	if _, err := s.pool.Exec(ctx, `UPDATE v3_identity.api_keys SET allowed_models=ARRAY['another-model'] WHERE id=$1`, k.ID); err != nil {
		t.Fatal(err)
	}
	configs = request(fmt.Sprintf("/api/desktop/tokens/%d/config", k.ID))["data"].(map[string]any)["tools"].(map[string]any)
	if configs["claude"].(map[string]any)["model"] != nil {
		t.Fatal("recommended a model blocked by key allowlist", configs["claude"])
	}
}

func seedDesktopStatusFacts(t *testing.T, s *Service, uid, cid int64, now time.Time) {
	t.Helper()
	ctx := context.Background()
	for i := range 8 {
		at := now.Add(-10 * time.Minute)
		if i < 4 {
			at = now.Add(-time.Hour)
		}
		status := "success"
		if i == 7 {
			status = "failed"
		}
		request := fmt.Sprintf("desktop-status-%d", i)
		if _, err := s.pool.Exec(ctx, `INSERT INTO v3_audit.request_audits(request_id,trace_id,user_id,key_id,model,group_name,protocol,request_type,status,counted_in_success_rate,billable,amount,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at,created_at,updated_at)
		 VALUES($1,'', $2,0,'claude-sonnet-4-5','default','anthropic','sync',$3,true,true,0,100,0,$4,1,0,200,'',$5,$5,$5,$5)`, request, uid, status, cid, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',$1,'wallet') ON CONFLICT(owner_type,owner_id,kind) DO NOTHING`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,prompt_tokens,cached_tokens,request_id,model)
	 SELECT $2,id,$1,$3,0,100,50,'desktop-status-4','claude-sonnet-4-5' FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind='wallet'`, uid, now, cid); err != nil {
		t.Fatal(err)
	}
}
