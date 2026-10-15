package http

import (
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type desktopReleaseLatestPayload struct {
	TagName     string `json:"tag_name"`
	Version     string `json:"version"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Notes       string `json:"notes"`
	HomebrewURL string `json:"homebrew_url"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Platform           string `json:"platform"`
		Arch               string `json:"arch"`
		Digest             string `json:"digest"`
	} `json:"assets"`
	Platforms map[string]struct {
		Signature string `json:"signature"`
		URL       string `json:"url"`
	} `json:"platforms"`
}

type desktopReleaseLatestJSONPayload struct {
	Version   string `json:"version"`
	Notes     string `json:"notes"`
	PubDate   string `json:"pub_date"`
	Platforms map[string]struct {
		Signature string `json:"signature"`
		URL       string `json:"url"`
	} `json:"platforms"`
}

func decodeDesktopReleasePayload[T any](t *testing.T, recorderBody []byte) T {
	t.Helper()
	var payload T
	if err := platformencoding.Unmarshal(recorderBody, &payload); err != nil {
		t.Fatalf("failed to decode desktop release payload: %v", err)
	}
	return payload
}

func setDesktopReleaseServerAddressForTest(t *testing.T, value string) {
	t.Helper()
	original := platformconfig.ServerAddress
	platformconfig.ServerAddress = value
	t.Cleanup(func() {
		platformconfig.ServerAddress = original
	})
}

func setDesktopReleaseGitHubServerForTest(t *testing.T, server *httptest.Server) {
	t.Helper()
	originalBaseURL := platformruntime.DesktopReleaseGitHubAPIBaseURL
	originalClient := platformruntime.DesktopReleaseHTTPClient
	platformruntime.DesktopReleaseGitHubAPIBaseURL = server.URL
	platformruntime.DesktopReleaseHTTPClient = server.Client()
	t.Cleanup(func() {
		platformruntime.DesktopReleaseGitHubAPIBaseURL = originalBaseURL
		platformruntime.DesktopReleaseHTTPClient = originalClient
	})
}

func TestGetDesktopReleaseLatestReturnsConfiguredManifest(t *testing.T) {
	t.Setenv("CODEGO_DESKTOP_RELEASE_GITHUB_FALLBACK_ENABLED", "false")
	t.Setenv(platformruntime.DesktopReleaseManifestJSONEnv, `{
		"tag_name":"v3.16.3",
		"html_url":"/download/releases/v3.16.3",
		"published_at":"2026-06-28T00:00:00Z",
		"notes":"Code Go Desktop stable release",
		"homebrew_url":"https://brew.example.test/codego-desktop",
		"assets":[
			{
				"name":"Code-Go-Desktop-v3.16.3-Windows.msi",
				"size":10485760,
				"digest":"sha256:windows",
				"platform":"windows",
				"arch":"x86_64",
				"browser_download_url":"/downloads/codego/windows.msi"
			}
		],
		"platforms":{
			"windows-x86_64":{
				"signature":"windows-signature",
				"url":"/updates/codego/windows-x86_64.zip"
			}
		}
	}`)
	t.Setenv(platformruntime.DesktopReleaseManifestFileEnv, "")
	setDesktopReleaseServerAddressForTest(t, "https://codegoai.com")

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/release/latest", nil, 0)
	GetDesktopReleaseLatest(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	payload := decodeDesktopReleasePayload[desktopReleaseLatestPayload](t, recorder.Body.Bytes())
	if payload.Version != "3.16.3" {
		t.Fatalf("expected version 3.16.3, got %q", payload.Version)
	}
}

func TestGetDesktopReleaseLatestJSONReturnsUpdaterManifest(t *testing.T) {
	t.Setenv("CODEGO_DESKTOP_RELEASE_GITHUB_FALLBACK_ENABLED", "false")
	t.Setenv(platformruntime.DesktopReleaseManifestJSONEnv, `{
		"version":"3.16.4",
		"html_url":"https://codegoai.com/download/releases/v3.16.4",
		"published_at":"2026-06-28T08:00:00Z",
		"notes":"Desktop updater test release",
		"assets":[],
		"platforms":{
			"windows-x86_64":{"signature":"sig-win","url":"https://downloads.example.test/windows.zip"},
			"darwin-aarch64":{"signature":"sig-mac","url":"https://downloads.example.test/macos.zip"}
		}
	}`)
	t.Setenv(platformruntime.DesktopReleaseManifestFileEnv, "")

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/release/latest.json", nil, 0)
	GetDesktopReleaseLatestJSON(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
}

func TestGetDesktopReleaseLatestReturnsServiceUnavailableWhenMissing(t *testing.T) {
	t.Setenv("CODEGO_DESKTOP_RELEASE_GITHUB_FALLBACK_ENABLED", "false")
	t.Setenv(platformruntime.DesktopReleaseManifestJSONEnv, "")
	t.Setenv(platformruntime.DesktopReleaseManifestFileEnv, "")

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/release/latest", nil, 0)
	GetDesktopReleaseLatest(ctx)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503 when manifest is missing, got %d", recorder.Code)
	}
}

func TestGetDesktopReleaseLatestUsesNewerGitHubReleaseFallback(t *testing.T) {
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/sh2001sh/CodeGO/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name":"v1.0.1",
			"name":"Code Go Desktop v1.0.1",
			"html_url":"https://github.com/sh2001sh/CodeGO/releases/tag/v1.0.1",
			"published_at":"2026-07-02T11:58:07Z",
			"assets":[
				{
					"name":"CodeGo_1.0.1_x64_zh-CN.msi",
					"size":16560128,
					"browser_download_url":"https://github.com/sh2001sh/CodeGO/releases/download/v1.0.1/CodeGo_1.0.1_x64_zh-CN.msi"
				},
				{
					"name":"CodeGo_1.0.1_x64.AppImage",
					"size":95377912,
					"browser_download_url":"https://github.com/sh2001sh/CodeGO/releases/download/v1.0.1/CodeGo_1.0.1_x64.AppImage"
				},
				{
					"name":"latest.json",
					"size":119,
					"browser_download_url":"https://github.com/sh2001sh/CodeGO/releases/download/v1.0.1/latest.json"
				}
			]
		}`))
	}))
	defer githubServer.Close()

	setDesktopReleaseGitHubServerForTest(t, githubServer)
	t.Setenv("CODEGO_DESKTOP_RELEASE_GITHUB_FALLBACK_ENABLED", "true")
	t.Setenv("CODEGO_DESKTOP_RELEASE_GITHUB_REPOSITORY", "sh2001sh/CodeGO")
	t.Setenv(platformruntime.DesktopReleaseManifestFileEnv, "")
	t.Setenv(platformruntime.DesktopReleaseManifestJSONEnv, `{
		"tag_name":"v3.16.4",
		"published_at":"2026-06-28T12:00:00Z",
		"notes":"Code Go Desktop v3.16.4",
		"assets":[
			{
				"name":"CodeGo_3.16.4_x64_en-US.msi",
				"size":10485760,
				"platform":"windows",
				"arch":"x64",
				"browser_download_url":"/downloads/codego/CodeGo_3.16.4_x64_en-US.msi"
			}
		],
		"platforms":{
			"windows-x86_64":{"signature":"sig-3164","url":"/downloads/codego/CodeGo_3.16.4_x64_en-US.msi"}
		}
	}`)

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/release/latest", nil, 0)
	GetDesktopReleaseLatest(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
}

func TestGetDesktopReleaseLatestUsesGitHubFallbackByDefault(t *testing.T) {
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/sh2001sh/CodeGO/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name":"v1.0.3",
			"name":"Code Go Desktop v1.0.3",
			"html_url":"https://github.com/sh2001sh/CodeGO/releases/tag/v1.0.3",
			"published_at":"2026-07-02T13:28:13Z",
			"assets":[
				{
					"name":"CodeGo_1.0.3_x64_zh-CN.msi",
					"size":16560128,
					"browser_download_url":"https://github.com/sh2001sh/CodeGO/releases/download/v1.0.3/CodeGo_1.0.3_x64_zh-CN.msi"
				}
			]
		}`))
	}))
	defer githubServer.Close()

	setDesktopReleaseGitHubServerForTest(t, githubServer)
	t.Setenv("CODEGO_DESKTOP_RELEASE_GITHUB_REPOSITORY", "sh2001sh/CodeGO")
	t.Setenv(platformruntime.DesktopReleaseManifestJSONEnv, `{
		"tag_name":"v3.16.4",
		"published_at":"2026-06-28T12:00:00Z",
		"notes":"Code Go Desktop v3.16.4",
		"assets":[
			{
				"name":"CodeGo_3.16.4_x64_en-US.msi",
				"size":10485760,
				"platform":"windows",
				"arch":"x64",
				"browser_download_url":"/downloads/codego/CodeGo_3.16.4_x64_en-US.msi"
			}
		],
		"platforms":{
			"windows-x86_64":{"signature":"sig-3164","url":"/downloads/codego/CodeGo_3.16.4_x64_en-US.msi"}
		}
	}`)

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/release/latest", nil, 0)
	GetDesktopReleaseLatest(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
}

func TestGetDesktopReleaseLatestJSONUsesGitHubLatestAssetWhenConfiguredManifestIsStale(t *testing.T) {
	var githubServer *httptest.Server
	githubServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/sh2001sh/CodeGO/releases/latest":
			_, _ = w.Write([]byte(`{
				"tag_name":"v1.0.5",
				"name":"Code Go Desktop v1.0.5",
				"html_url":"https://github.com/sh2001sh/CodeGO/releases/tag/v1.0.5",
				"published_at":"2026-07-02T15:52:20Z",
				"assets":[
					{
						"name":"CodeGo_1.0.5_x64_zh-CN.msi",
						"size":16560128,
						"browser_download_url":"https://github.com/sh2001sh/CodeGO/releases/download/v1.0.5/CodeGo_1.0.5_x64_zh-CN.msi"
					},
					{
						"name":"latest.json",
						"size":256,
						"browser_download_url":"` + githubServer.URL + `/downloads/latest.json"
					}
				]
			}`))
		case "/downloads/latest.json":
			_, _ = w.Write([]byte(`{
				"version":"1.0.5",
				"notes":"Code Go Desktop v1.0.5",
				"pub_date":"2026-07-02T15:52:20Z",
				"platforms":{
					"windows-x86_64":{
						"signature":"sig-105",
						"url":"/downloads/codego/CodeGo_1.0.5_x64_zh-CN.msi"
					}
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubServer.Close()

	setDesktopReleaseGitHubServerForTest(t, githubServer)
	t.Setenv("CODEGO_DESKTOP_RELEASE_GITHUB_REPOSITORY", "sh2001sh/CodeGO")
	t.Setenv(platformruntime.DesktopReleaseManifestJSONEnv, `{
		"tag_name":"v3.16.4",
		"published_at":"2026-06-28T12:00:00Z",
		"notes":"Code Go Desktop v3.16.4",
		"assets":[
			{
				"name":"CodeGo_3.16.4_x64_en-US.msi",
				"size":10485760,
				"platform":"windows",
				"arch":"x64",
				"browser_download_url":"/downloads/codego/CodeGo_3.16.4_x64_en-US.msi"
			}
		],
		"platforms":{
			"windows-x86_64":{"signature":"sig-3164","url":"/downloads/codego/CodeGo_3.16.4_x64_en-US.msi"}
		}
	}`)

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/release/latest.json", nil, 0)
	GetDesktopReleaseLatestJSON(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
}

func TestGetDesktopReleaseLatestReloadsManifestFileWithoutRestart(t *testing.T) {
	t.Setenv("CODEGO_DESKTOP_RELEASE_GITHUB_FALLBACK_ENABLED", "false")
	manifestPath := filepath.Join(t.TempDir(), "codego-desktop-release-manifest.json")
	writeManifest := func(contents string) {
		t.Helper()
		if err := os.WriteFile(manifestPath, []byte(contents), 0o600); err != nil {
			t.Fatalf("failed to write manifest fixture: %v", err)
		}
	}

	writeManifest(`{
		"tag_name":"v3.16.4",
		"published_at":"2026-06-28T12:00:00Z",
		"notes":"Code Go Desktop v3.16.4",
		"assets":[
			{
				"name":"CodeGo_3.16.4_x64_en-US.msi",
				"size":10485760,
				"digest":"sha256:v3164",
				"platform":"windows",
				"arch":"x64",
				"browser_download_url":"/downloads/codego/CodeGo_3.16.4_x64_en-US.msi"
			}
		],
		"platforms":{
			"windows-x86_64":{
				"signature":"sig-3164",
				"url":"/downloads/codego/CodeGo_3.16.4_x64_en-US.msi"
			}
		}
	}`)

	t.Setenv(platformruntime.DesktopReleaseManifestJSONEnv, "")
	t.Setenv(platformruntime.DesktopReleaseManifestFileEnv, manifestPath)
	setDesktopReleaseServerAddressForTest(t, "https://codegoai.com")

	firstCtx, firstRecorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/release/latest", nil, 0)
	GetDesktopReleaseLatest(firstCtx)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("expected first HTTP 200, got %d", firstRecorder.Code)
	}

	writeManifest(`{
		"tag_name":"v3.16.3",
		"published_at":"2026-06-20T08:30:00Z",
		"notes":"Code Go Desktop rollback target",
		"assets":[
			{
				"name":"CodeGo_3.16.3_x64_en-US.msi",
				"size":9437184,
				"digest":"sha256:v3163",
				"platform":"windows",
				"arch":"x64",
				"browser_download_url":"/downloads/codego/CodeGo_3.16.3_x64_en-US.msi"
			}
		],
		"platforms":{
			"windows-x86_64":{
				"signature":"sig-3163",
				"url":"/downloads/codego/CodeGo_3.16.3_x64_en-US.msi"
			}
		}
	}`)

	secondCtx, secondRecorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/release/latest", nil, 0)
	GetDesktopReleaseLatest(secondCtx)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("expected second HTTP 200, got %d", secondRecorder.Code)
	}
}
