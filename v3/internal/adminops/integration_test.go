//go:build pgintegration

package adminops

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func adminopsPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_ADMINOPS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_ADMINOPS_TEST_PG_DSN not set (requires an isolated empty database)")
	}
	ctx := context.Background()
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	// This variable is deliberately separate from shared integration storage.
	if p.Config().ConnConfig.Database != "adminops_tests" || p.Config().ConnConfig.Host != "127.0.0.1" {
		t.Fatal("adminops fixture requires loopback database adminops_tests")
	}
	var existing int
	if e = p.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'v3\_%' ESCAPE '\'`).Scan(&existing); e != nil {
		t.Fatal(e)
	}
	if existing > 0 {
		rows, e := p.Query(ctx, `SELECT nspname FROM pg_namespace WHERE nspname LIKE 'v3\_%' ESCAPE '\'`)
		if e != nil {
			t.Fatal(e)
		}
		var schemas []string
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				t.Fatal(e)
			}
			schemas = append(schemas, name)
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		for _, schema := range schemas {
			if _, e = p.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); e != nil {
				t.Fatal(e)
			}
		}
	}
	names, e := migrations.Files()
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range names {
		if n > "20261001000072_zz.sql" && n != "20261001000076_adminops.sql" {
			continue
		}
		sql, e := migrations.Read(n)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Exec(ctx, sql); e != nil {
			t.Fatalf("migration %s: %v", n, e)
		}
	}
	return p
}
func TestAdminOpsPostgresOwnershipConcurrencyPricingAndDeployments(t *testing.T) {
	p := adminopsPool(t)
	ctx := context.Background()
	enc, e := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(1,'tools-user-1'),(2,'tools-user-2');INSERT INTO v3_catalog.models(id,model_name) VALUES(11,'m1'),(12,'m2');`); e != nil {
		t.Fatal(e)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "encrypted-test-key" {
			t.Error("provider credential missing")
		}
		switch r.URL.Path {
		case "/hardware/max-gpus-per-container":
			_, _ = w.Write([]byte(`{"data":{"hardware":[{"hardware_id":1,"hardware_name":"A100","available":3,"max_gpus_per_container":4}],"total":3}}`))
		case "/deployments":
			_, _ = w.Write([]byte(`{"data":{"deployments":[{"id":"dep1","name":"fixture","status":"running","hardware_quantity":1}],"total":1}}`))
		case "/deployment/dep1":
			if r.Method == "GET" {
				_, _ = w.Write([]byte(`{"data":{"id":"dep1","status":"running","created_at":"2026-10-01T12:00:00","container_config":{"env_variables":{"TOKEN":"must-redact","MODE":"prod"}}}}`))
			} else {
				_, _ = w.Write([]byte(`{"status":"accepted","deployment_id":"dep1"}`))
			}
		case "/deploy":
			_, _ = w.Write([]byte(`{"status":"accepted","deployment_id":"dep2"}`))
		case "/locations":
			_, _ = w.Write([]byte(`{"data":{"locations":[{"id":1,"name":"Test location","iso2":"us","available":2}],"total":1}}`))
		case "/available-replicas":
			_, _ = w.Write([]byte(`{"data":[{"id":1,"name":"Test location","iso2":"US","available_replicas":2}]}`))
		case "/price":
			_, _ = w.Write([]byte(`{"data":{"replica_count":1,"gpus_per_container":1,"total_cost_usdc":4,"ionet_fee":1}}`))
		case "/clusters/check_cluster_name_availability":
			_, _ = w.Write([]byte(`true`))
		case "/clusters/dep1/update-name":
			_, _ = w.Write([]byte(`{"status":"accepted","message":"Updated"}`))
		case "/deployment/dep1/extend":
			_, _ = w.Write([]byte(`{"data":{"id":"dep1","status":"running","created_at":"2026-10-01T12:00:00"}}`))
		case "/deployment/dep1/containers":
			_, _ = w.Write([]byte(`{"data":{"total":1,"workers":[{"container_id":"c1","status":"running","created_at":"2026-10-01T12:00:00","container_events":[{"time":"2026-10-01T12:00:00","message":"Started"}]}]}}`))
		case "/deployment/dep1/container/c1":
			_, _ = w.Write([]byte(`{"container_id":"c1","status":"running","created_at":"2026-10-01T12:00:00","container_events":[{"time":"2026-10-01T12:00:00","message":"Started"}]}`))
		case "/deployment/dep1/log/c1":
			_, _ = w.Write([]byte(`ordinary log contains encrypted-test-key`))
		case "/deployment/fail":
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"detail":"encrypted-test-key"}`))
		case "/deployment/bad":
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer provider.Close()
	s := New(p, enc, Config{DeploymentEnterpriseURL: provider.URL, DeploymentPublicURL: provider.URL}, nil)
	mux1 := http.NewServeMux()
	s.Register(mux1, func(*http.Request) (Actor, error) { return Actor{UserID: 1, Role: "root"}, nil })
	mux2 := http.NewServeMux()
	s.Register(mux2, func(*http.Request) (Actor, error) { return Actor{UserID: 2, Role: "user"}, nil })
	testHTTP(t, mux1, "PUT", "/api/models/favorites/", `{"model_id":11,"favorite":true}`, 200)
	if got := testHTTP(t, mux1, "GET", "/api/models/favorites/", "", 200).Body.String(); !strings.Contains(got, `"models":[{"id":11,"model_name":"m1"}]`) {
		t.Fatal("favorite model names missing", got)
	}
	testHTTP(t, mux2, "GET", "/api/models/favorites/", "", 200)
	if got := testHTTP(t, mux2, "GET", "/api/models/favorites/", "", 200).Body.String(); !strings.Contains(got, `"model_ids":[]`) {
		t.Fatal("favorite ownership leaked", got)
	}
	var wg sync.WaitGroup
	errs := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := `{"model_id":11,"favorite":true}`
			if i%2 == 0 {
				body = `{"model_id":12,"favorite":true}`
			}
			w := httptest.NewRecorder()
			mux1.ServeHTTP(w, httptest.NewRequest("PUT", "/api/models/favorites/", strings.NewReader(body)))
			if w.Code != 200 {
				errs <- w.Body.String()
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := testHTTP(t, mux1, "GET", "/api/models/favorites/", "", 200).Body.String(); !strings.Contains(got, `"model_ids":[11,12]`) {
		t.Fatal("concurrent favorite lost", got)
	}
	testHTTP(t, mux1, "PUT", "/api/models/favorites/", `{"model_id":999,"favorite":true}`, 404)
	testHTTP(t, mux1, "PUT", "/api/models/favorites/", `{"model_id":11,"favorite":false}`, 200)
	var count int
	if e = p.QueryRow(ctx, `SELECT count(*) FROM v3_adminops.model_favorites WHERE user_id=1`).Scan(&count); e != nil || count != 1 {
		t.Fatalf("favorites count=%d err=%v", count, e)
	}
	testHTTP(t, mux1, "POST", "/api/ratio_sync/apply", `{"prices":[{"model":"precise","input_per_mtok":9007199254740993,"output_per_mtok":3}]}`, 200)
	var amount int64
	if e = p.QueryRow(ctx, `SELECT input_per_mtok FROM v3_catalog.model_prices WHERE model='precise'`).Scan(&amount); e != nil || amount != 9007199254740993 {
		t.Fatalf("price lost precision: %d %v", amount, e)
	}
	testHTTP(t, mux1, "POST", "/api/ratio_sync/apply", `{"prices":[{"model":"should-not-save","input_per_mtok":1},{"model":"bad","input_per_mtok":-1}]}`, 400)
	if e = p.QueryRow(ctx, `SELECT count(*) FROM v3_catalog.model_prices WHERE model='should-not-save'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial invalid pricing applied", count, e)
	}
	testHTTP(t, mux1, "GET", "/api/deployments/", "", 503)
	ciphertext, e := enc.Encrypt([]byte(`"encrypted-test-key"`))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO v3_platform.settings(key,value) VALUES('model_deployment.ionet.enabled','true')`); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO v3_platform.settings(key,ciphertext,sensitive) VALUES('model_deployment.ionet.api_key',$1,true)`, ciphertext); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/deployments/settings", "/api/deployments/", "/api/deployments/hardware-types", "/api/deployments/dep1"} {
		w := testHTTP(t, mux1, "GET", path, "", 200)
		if strings.Contains(w.Body.String(), "encrypted-test-key") || strings.Contains(w.Body.String(), "must-redact") {
			t.Fatalf("provider secret leaked from %s: %s", path, w.Body.String())
		}
	}
	testHTTP(t, mux1, "GET", "/api/deployments/fail", "", 502)
	testHTTP(t, mux1, "GET", "/api/deployments/bad", "", 502)
	testHTTP(t, mux1, "GET", "/api/deployments/a.b", "", 400)
	testHTTP(t, mux1, "POST", "/api/deployments/", `{"resource_private_name":"fixture","duration_hours":1,"gpus_per_container":1,"hardware_id":1,"location_ids":[1],"container_config":{"replica_count":1},"registry_config":{"image_url":"public/image:latest"}}`, 200)
	testHTTP(t, mux1, "DELETE", "/api/deployments/dep1", "", 200)
	for _, path := range []string{"/api/deployments/search?keyword=fixture", "/api/deployments/locations", "/api/deployments/available-replicas?hardware_id=1&gpu_count=1", "/api/deployments/check-name?name=new-name", "/api/deployments/dep1/containers", "/api/deployments/dep1/containers/c1", "/api/deployments/dep1/logs?container_id=c1"} {
		w := testHTTP(t, mux1, "GET", path, "", 200)
		if strings.Contains(w.Body.String(), "encrypted-test-key") {
			t.Fatalf("credential exposed from %s", path)
		}
	}
	testHTTP(t, mux1, "POST", "/api/deployments/test-connection", `{}`, 200)
	testHTTP(t, mux1, "POST", "/api/deployments/price-estimation", `{"location_ids":[1],"hardware_id":1,"gpus_per_container":1,"duration_hours":1,"replica_count":1}`, 200)
	testHTTP(t, mux1, "PUT", "/api/deployments/dep1", `{"traffic_port":8080}`, 200)
	testHTTP(t, mux1, "PUT", "/api/deployments/dep1/name", `{"name":"new-name"}`, 200)
	testHTTP(t, mux1, "POST", "/api/deployments/dep1/extend", `{"duration_hours":1}`, 200)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"model_ratio":{"m":2},"completion_ratio":{"m":3}}}`))
	}))
	defer upstream.Close()
	body, _ := json.Marshal(UpstreamRequest{Upstreams: []UpstreamDTO{{Name: "fixture", BaseURL: upstream.URL}}})
	w := testHTTP(t, mux1, "POST", "/api/ratio_sync/fetch", string(body), 200)
	if !strings.Contains(w.Body.String(), `"status":"success"`) || !strings.Contains(w.Body.String(), `"model_ratio"`) {
		t.Fatal(w.Body.String())
	}
	openRouter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer router-test-key" {
			t.Error("OpenRouter endpoint or channel authorization was lost")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"router-model","pricing":{"prompt":"0.000002","completion":"0.000006"}}]}`))
	}))
	defer openRouter.Close()
	routerKey, e := enc.Encrypt([]byte("router-test-key"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO v3_catalog.channels(id,name,provider,base_url) VALUES(99,'router-fixture','openrouter',$1)`, openRouter.URL); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO v3_catalog.channel_credentials(channel_id,secret) VALUES(99,$1)`, routerKey); e != nil {
		t.Fatal(e)
	}
	w = testHTTP(t, mux1, "POST", "/api/ratio_sync/fetch", `{"channel_ids":[99]}`, 200)
	if !strings.Contains(w.Body.String(), `"status":"success"`) {
		t.Fatal(w.Body.String())
	}
	attacker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("spoofed upstream received a request") }))
	defer attacker.Close()
	body, _ = json.Marshal(UpstreamRequest{Upstreams: []UpstreamDTO{{ID: 99, Name: "spoof", BaseURL: attacker.URL, Endpoint: "openrouter"}}})
	w = testHTTP(t, mux1, "POST", "/api/ratio_sync/fetch", string(body), 200)
	if !strings.Contains(w.Body.String(), `"status":"error"`) || strings.Contains(w.Body.String(), "router-test-key") {
		t.Fatal("spoofed channel accepted or secret leaked", w.Body.String())
	}
}
