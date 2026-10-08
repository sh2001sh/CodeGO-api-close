package adminops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/adminops/ionet"
)

func testHTTP(t *testing.T, h http.Handler, method, path, body string, want int) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	if w.Code != want {
		t.Fatalf("%s %s = %d want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	return w
}
func adminMux(s *Server, role string) *http.ServeMux {
	m := http.NewServeMux()
	s.Register(m, func(*http.Request) (Actor, error) { return Actor{UserID: 1, Role: role}, nil })
	return m
}
func TestPermissionsAndInputValidation(t *testing.T) {
	s := New(nil, nil, Config{}, nil)
	m := http.NewServeMux()
	s.Register(m, nil)
	testHTTP(t, m, "GET", "/api/models/favorites/", "", 401)
	testHTTP(t, m, "GET", "/api/deployments/", "", 401)
	testHTTP(t, m, "GET", "/api/performance/stats", "", 401)
	user := adminMux(s, "user")
	testHTTP(t, user, "GET", "/api/ratio_sync/channels", "", 403)
	testHTTP(t, user, "GET", "/api/performance/stats", "", 403)
	testHTTP(t, user, "POST", "/api/deployments/", "{}", 403)
	testHTTP(t, user, "PUT", "/api/models/favorites/", `{"model_id":0}`, 400)
	admin := adminMux(s, "admin")
	testHTTP(t, admin, "GET", "/api/performance/stats", "", 403)
	testHTTP(t, admin, "POST", "/api/deployments/test-connection", `{"api_key":"replacement-secret"}`, 403)
	root := adminMux(s, "root")
	testHTTP(t, root, "POST", "/api/deployments/", `{"hardware_id":1}`, 400)
	testHTTP(t, root, "POST", "/api/deployments/", `{} {}`, 400)
	testHTTP(t, root, "POST", "/api/deployments/", `{"unknown":1}`, 400)
	testHTTP(t, root, "GET", "/api/deployments/available-replicas?hardware_id=x", "", 400)
	testHTTP(t, root, "POST", "/api/ratio_sync/apply", `{"prices":[]}`, 400)
	testHTTP(t, root, "POST", "/api/ratio_sync/fetch", `{"upstreams":[{"name":"x","base_url":"http://169.254.169.254"}]}`, 400)
	req := httptest.NewRequest("POST", "/api/performance/gc", nil)
	req.Header.Set("Origin", "https://attacker.invalid")
	w := httptest.NewRecorder()
	root.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestRatioUpstreamFailureTimeoutAndConversions(t *testing.T) {
	ups, err := New(nil, nil, Config{}, nil).collectUpstreams(context.Background(), UpstreamRequest{ChannelIDs: []int64{-100, -101, -100}})
	if err != nil || len(ups) != 2 || ups[0].Endpoint != "/llm-metadata/api/newapi/ratio_config-v1-base.json" || ups[1].Endpoint != "/api.json" {
		t.Fatalf("preset restoration failed: %+v %v", ups, err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(`{"success":true,"data":{"model_ratio":{"model":1.5},"completion_ratio":{"model":2}}}`))
		case "/bad":
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`secret-key-from-provider`))
		case "/invalid":
			_, _ = w.Write([]byte(`{"success":true,"data":{"model_ratio":{"model":-1}}}`))
		case "/slow":
			<-r.Context().Done()
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		}
	}))
	defer upstream.Close()
	s := New(nil, nil, Config{}, nil)
	result := s.fetchRatio(context.Background(), UpstreamDTO{Name: "demo", BaseURL: upstream.URL, Endpoint: "ok"}, 1)
	if result.Err != "" || result.Data["model_ratio"].(map[string]any)["model"] != 1.5 {
		t.Fatalf("%+v", result)
	}
	for _, path := range []string{"bad", "invalid", "redirect", "slow"} {
		r := s.fetchRatio(context.Background(), UpstreamDTO{Name: "demo", BaseURL: upstream.URL, Endpoint: path}, 1)
		if r.Err == "" || strings.Contains(r.Err, "secret-key") {
			t.Fatalf("%s %+v", path, r)
		}
	}
	openrouter, err := decodeRatio([]byte(`{"data":[{"id":"m","pricing":{"prompt":"0.000002","completion":"0.000006","input_cache_read":"0.000001"}}]}`), true, false)
	if err != nil || openrouter["model_ratio"].(map[string]any)["m"] != 1.0 || openrouter["completion_ratio"].(map[string]any)["m"] != 3.0 {
		t.Fatalf("%+v %v", openrouter, err)
	}
	modelsdev, err := decodeRatio([]byte(`{"vendor":{"models":{"m":{"cost":{"input":2,"output":6,"cache_read":1}}}}}`), false, true)
	if err != nil || modelsdev["model_ratio"].(map[string]any)["m"] != 1.0 {
		t.Fatalf("%+v %v", modelsdev, err)
	}
	if _, err = decodeRatio([]byte(`{"success":true,"data":{"model_ratio":{"m":{}}}}`), false, false); err == nil {
		t.Fatal("non-numeric ratio accepted")
	}
	if _, err = decodeRatio([]byte(`{"data":[{"id":"m","pricing":{"prompt":"NaN","completion":"1"}}]}`), true, false); err == nil {
		t.Fatal("NaN provider price accepted")
	}
}
func TestDeploymentProviderCancellationAndRedaction(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "test-secret" {
			t.Error("missing credential")
		}
		switch r.URL.Path {
		case "/deployment/fail":
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"detail":"test-secret"}`))
		case "/wait":
			<-r.Context().Done()
		default:
			_, _ = w.Write([]byte(`{"data":{"hardware":[],"total":0}}`))
		}
	}))
	defer upstream.Close()
	client := providerHTTP{client: defaultToolClient(), credential: "test-secret"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Do(&ionet.HTTPRequest{Context: ctx, Method: "GET", URL: upstream.URL + "/wait", Headers: map[string]string{"X-API-KEY": "test-secret"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	s := New(nil, nil, Config{DeploymentEnterpriseURL: upstream.URL}, nil)
	c := s.makeDeploymentClient(context.Background(), "test-secret", false)
	_, err = c.GetDeployment("fail")
	if err == nil {
		t.Fatal("upstream rejection accepted")
	}
	if strings.Contains(err.Error(), "test-secret") {
		t.Fatal("provider secret leaked")
	}
	data := redact(map[string]any{"id": int64(9007199254740993), "container_config": map[string]any{"secret_env_variables": map[string]string{"KEY": "secret"}, "registry_secret": "secret", "env_variables": map[string]string{"MODE": "prod"}}})
	raw, _ := json.Marshal(data)
	if strings.Contains(string(raw), "secret") || !bytes.Contains(raw, []byte("9007199254740993")) {
		t.Fatalf("unsafe redaction: %s", raw)
	}
	credential := `test-"escaped"-key`
	clean := redactCredentialBody([]byte(`{"message":"test-\"escaped\"-key","id":9007199254740993}`), credential)
	if strings.Contains(string(clean), "escaped") || !bytes.Contains(clean, []byte("9007199254740993")) {
		t.Fatalf("escaped secret or precision lost: %s", clean)
	}
}
func TestPerformanceRetentionPreservesActiveAndRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"codego-active.log", "codego-old.log", "unrelated.txt"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-time.Duration(i+1) * 24 * time.Hour)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	s := New(nil, nil, Config{LogDir: dir, ActiveLog: func() string { return filepath.Join(dir, "codego-active.log") }, Now: func() time.Time { return now }}, nil)
	m := adminMux(s, "root")
	testHTTP(t, m, "DELETE", "/api/performance/logs?mode=by_count&value=1", "", 200)
	if _, err := os.Stat(filepath.Join(dir, "codego-old.log")); !os.IsNotExist(err) {
		t.Fatalf("old log remains: %v", err)
	}
	for _, name := range []string{"codego-active.log", "unrelated.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	testHTTP(t, m, "DELETE", "/api/performance/logs?mode=by_count&value=0", "", 400)
	testHTTP(t, m, "DELETE", "/api/performance/disk_cache", "", 409)
	if err := os.Symlink(filepath.Join(dir, "unrelated.txt"), filepath.Join(dir, "codego-symlink.log")); err == nil {
		files, err := directoryFiles(dir, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("symlink accepted: %+v", files)
		}
	}
	result, err := removeFiles(dir, []LogFileInfo{{Name: "../unrelated.txt"}}, "")
	if err == nil || result.DeletedCount != 0 {
		t.Fatal("path traversal accepted")
	}
}
