package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopReleaseUsesRealUpdaterAssetsAndSurfacesProviderFailure(t *testing.T) {
	t.Setenv("CODEGO_DESKTOP_RELEASE_GITHUB_REPOSITORY", "fixture/desktop")
	providerStatus := 200
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if providerStatus != 200 {
			w.WriteHeader(providerStatus)
			return
		}
		switch r.URL.Path {
		case "/repos/fixture/desktop/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.2.3", "name": "release notes", "html_url": server.URL, "assets": []map[string]any{{"name": "latest.json", "browser_download_url": server.URL + "/latest.json"}, {"name": "CodeGo_1.2.3_x64-setup.exe", "browser_download_url": server.URL + "/CodeGo_1.2.3_x64-setup.exe", "size": 512}}})
		case "/latest.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.2.3", "notes": "signed updater", "pub_date": "2026-10-02T00:00:00Z", "platforms": map[string]any{"windows-x86_64": map[string]any{"signature": "fixture-signature", "url": server.URL + "/CodeGo_1.2.3_x64-setup.exe"}}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	manifest, err := githubRelease(context.Background(), server.Client(), server.URL)
	if err != nil || manifest.Version != "1.2.3" || len(manifest.Assets) != 1 || manifest.Platforms["windows-x86_64"].Signature != "fixture-signature" {
		t.Fatalf("real updater assets missing: %+v %v", manifest, err)
	}
	providerStatus = 500
	if _, err := githubRelease(context.Background(), server.Client(), server.URL); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatal("provider failure hidden", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := githubRelease(ctx, server.Client(), server.URL); !errors.Is(err, context.Canceled) {
		t.Fatal("request ignored cancellation", err)
	}
}

func TestDesktopReleaseHTTPKeepsRawManifestAndMissingConfigurationFails(t *testing.T) {
	t.Setenv("CODEGO_DESKTOP_RELEASE_MANIFEST_JSON", "")
	t.Setenv("CODEGO_DESKTOP_RELEASE_MANIFEST_FILE", "")
	t.Setenv("CODEGO_DESKTOP_RELEASE_GITHUB_FALLBACK_ENABLED", "false")
	s := New(nil, nil, Config{})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/desktop/release/latest", nil))
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), `"success":true`) {
		t.Fatalf("unconfigured release fabricated: %d %s", w.Code, w.Body.String())
	}
	s.cfg.ReleaseManifest = func(context.Context) (any, error) {
		return ReleaseManifest{Version: "1.2.3", Platforms: map[string]ReleasePlatform{"windows-x86_64": {Signature: "fixture", URL: "https://example.com/update.exe"}}}, nil
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/desktop/release/latest.json", nil))
	var body map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body["version"] != "1.2.3" || body["success"] != nil {
		t.Fatalf("updater manifest envelope changed: %s", w.Body.String())
	}
}
