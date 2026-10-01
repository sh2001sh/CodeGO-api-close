//go:build pgintegration

package catalogcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestLegacyProbeControlIntegration(t *testing.T) {
	pool := testPool(t)
	enc, err := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-only-secret" {
			t.Error("decrypted credential was not supplied")
		}
		if strings.HasPrefix(r.URL.Path, "/bad/") {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"fixture-only-secret"}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"upstream-a"}]}`))
			return
		}
		_, _ = w.Write([]byte(probeChatResponse))
	}))
	defer upstream.Close()
	mux := http.NewServeMux()
	New(pool, enc, nil).Register(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer fixture-admin" {
				fail(w, 403, "forbidden", "Administrator authorization is required")
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	call := func(method, path, body string, authorized bool, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if authorized {
			r.Header.Set("Authorization", "Bearer fixture-admin")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s returned %d: %s", method, path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "fixture-only-secret") {
			t.Fatal("upstream credential leaked in API response")
		}
		return w
	}
	call("PUT", "/api/catalog/groups/default", `{"multiplier":1}`, true, 200)
	create := func(name, prefix, status string, withCredential bool) int64 {
		t.Helper()
		c := Channel{Name: name, Provider: "openai", BaseURL: upstream.URL + prefix, Status: status, Models: []string{name}, Groups: []string{"default"},
			Settings: json.RawMessage(`{}`), ParamOverride: json.RawMessage(`{}`), StatusCodeMapping: json.RawMessage(`{}`)}
		if withCredential {
			c.Credentials = []CredentialInput{{Secret: "fixture-only-secret", Kind: "api_key"}}
		}
		body, e := json.Marshal(c)
		if e != nil {
			t.Fatal(e)
		}
		w := call("POST", "/api/catalog/channels", string(body), true, 200)
		var result struct {
			Data struct {
				ID int64 `json:"id"`
			} `json:"data"`
		}
		if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil {
			t.Fatal(e)
		}
		return result.Data.ID
	}
	good := create("good-model", "/good", "enabled", true)
	bad := create("bad-model", "/bad", "enabled", true)
	create("disabled-model", "/good", "disabled", true)
	create("orphan-model", "/good", "disabled", false)
	goodID := strconv.FormatInt(good, 10)
	before := calls.Load()
	call("GET", "/api/channel/test/"+goodID, "", false, 403)
	call("POST", "/api/channel/fetch_models", `{"type":1,"base_url":"`+upstream.URL+`","key":"fixture-only-secret"}`, false, 403)
	if calls.Load() != before {
		t.Fatal("unauthorized probe contacted upstream")
	}
	w := call("GET", "/api/channel/test/"+goodID, "", true, 200)
	var result probeResult
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.Success {
		t.Fatalf("good probe %s", w.Body.String())
	}
	w = call("GET", "/api/channel/test/"+strconv.FormatInt(bad, 10), "", true, 200)
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Success || result.ErrorCode == "" {
		t.Fatalf("failed probe %s", w.Body.String())
	}
	w = call("GET", "/api/channel/test", "", true, 200)
	var all struct {
		Success bool `json:"success"`
		Tested  int  `json:"tested"`
		Failed  int  `json:"failed"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &all); err != nil || all.Success || all.Tested != 2 || all.Failed != 1 {
		t.Fatalf("all probes %s", w.Body.String())
	}
	w = call("GET", "/api/channel/fetch_models/"+goodID, "", true, 200)
	if !strings.Contains(w.Body.String(), "upstream-a") {
		t.Fatal("missing upstream model")
	}
	call("GET", "/api/channel/fetch_models/"+strconv.FormatInt(bad, 10), "", true, 502)
	w = call("GET", "/api/channel/models_enabled", "", true, 200)
	if strings.Contains(w.Body.String(), "disabled-model") || strings.Contains(w.Body.String(), "orphan-model") || !strings.Contains(w.Body.String(), "good-model") {
		t.Fatalf("enabled models %s", w.Body.String())
	}
	w = call("GET", "/api/channel/models", "", true, 200)
	if !strings.Contains(w.Body.String(), "disabled-model") || !strings.Contains(w.Body.String(), `"object":"model"`) {
		t.Fatalf("catalog model listing %s", w.Body.String())
	}
	if _, err = pool.Exec(context.Background(), `UPDATE v3_catalog.channel_credentials SET expires_at=now()-interval '1 minute' WHERE channel_id=$1`, good); err != nil {
		t.Fatal(err)
	}
	before = calls.Load()
	w = call("GET", "/api/channel/test/"+goodID, "", true, 200)
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Success || calls.Load() != before {
		t.Fatalf("expired credential probe %s", w.Body.String())
	}
	w = call("GET", "/api/channel/models_enabled", "", true, 200)
	if strings.Contains(w.Body.String(), "good-model") {
		t.Fatal("expired credential remained enabled for model listing")
	}
}
