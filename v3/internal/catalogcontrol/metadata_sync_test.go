package catalogcontrol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMetadataSyncSourceRetainsLocaleAndRejectsCredentialURL(t *testing.T) {
	t.Setenv("SYNC_UPSTREAM_BASE", "https://metadata.example/base/")
	for _, tc := range []struct{ locale, path string }{
		{"ZH-CN", "/base/api/i18n/zh-cn/newapi/models.json"},
		{"ja", "/base/api/i18n/ja/newapi/models.json"},
		{"unsupported", "/base/api/newapi/models.json"},
	} {
		source, err := metadataSource(tc.locale)
		if err != nil || source.ModelsURL != "https://metadata.example"+tc.path {
			t.Fatalf("source locale resolution: %+v %v", source, err)
		}
	}
	for _, address := range []string{"https://fixture-secret@metadata.example", "https://metadata.example?key=fixture-secret", "file:///local", "https://metadata.example#fixture-secret"} {
		t.Setenv("SYNC_UPSTREAM_BASE", address)
		_, err := metadataSource("en")
		if err == nil || strings.Contains(err.Error(), "fixture-secret") {
			t.Fatalf("unsafe source accepted or disclosed: %v", err)
		}
	}
}

func TestMetadataSyncFetchRejectsFailuresMalformedOversizedAndRedirect(t *testing.T) {
	for _, tc := range []struct {
		name, document string
		status         int
		wantSuccess    bool
	}{
		{"array", `[ {"model_name":"gpt"} ]`, 200, true},
		{"envelope", `{"success":true,"data":[{"model_name":"gpt"}]}`, 200, true},
		{"explicit failure", `{"success":false,"message":"fixture-secret","data":[]}`, 200, false},
		{"malformed", `{"data":`, 200, false},
		{"null", `null`, 200, false},
		{"trailing", `[] {}`, 200, false},
		{"unauthorized", `fixture-secret`, 401, false},
		{"oversized", `[` + strings.Repeat(" ", (10<<20)+1) + `]`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.document))
			}))
			defer server.Close()
			items, err := fetchMetadataJSON[metadataUpstreamModel](context.Background(), server.Client(), server.URL)
			if (err == nil) != tc.wantSuccess || (err != nil && strings.Contains(err.Error(), "fixture-secret")) {
				t.Fatalf("fetch outcome: %v, items %+v", err, items)
			}
		})
	}
	var redirected atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Store(true)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer server.Close()
	_, _, err := fetchMetadata(context.Background(), metadataSyncSource{ModelsURL: server.URL, VendorsURL: server.URL})
	if err == nil || redirected.Load() {
		t.Fatal("sync followed untrusted redirect")
	}
}

func TestMetadataSyncDoesNotSilentlyIgnoreInvalidVendorDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "models") {
			_, _ = w.Write([]byte(`[{"model_name":"gpt"}]`))
		} else {
			_, _ = w.Write([]byte(`broken vendor file`))
		}
	}))
	defer server.Close()
	_, _, err := fetchMetadata(context.Background(), metadataSyncSource{ModelsURL: server.URL + "/models", VendorsURL: server.URL + "/vendors"})
	if err == nil {
		t.Fatal("bad vendor response was swallowed")
	}
}
