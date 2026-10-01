package identitydto

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestLegacyKeyPoliciesConvertExactlyAndRejectConflicts(t *testing.T) {
	var got map[string]json.RawMessage
	h := Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	r := httptest.NewRequest("POST", "/api/token/", strings.NewReader(`{"name":"finite","status":1,"remain_quota":500000,"unlimited_quota":false,"model_limits_enabled":true,"model_limits":"model-a,model-b","allow_ips":"127.0.0.1\n10.0.0.0/8","expired_time":-1,"marketplace_multiplier_limit":1.234567}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || string(got["budget_micro_credits"]) != "1000000" || string(got["max_marketplace_multiplier_ppm"]) != "1234567" || string(got["budget_limited"]) != "true" || string(got["allowed_cidrs"]) != `["127.0.0.1/32","10.0.0.0/8"]` {
		t.Fatalf("status=%d body=%s converted=%v", w.Code, w.Body, got)
	}
	for _, body := range []string{
		`{"remain_quota":` + strconv.FormatInt(math.MaxInt64, 10) + `}`,
		`{"remain_quota":-1}`, `{"status":100}`, `{"allow_ips":"evil"}`,
		`{"marketplace_multiplier_limit":1.0000001}`, `{"remain_quota":1,"budget_micro_credits":2}`,
		`{"name":"first"}{"name":"second"}`,
	} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/token/", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("invalid=%s code=%d", body, w.Code)
		}
	}
}

func TestDefaultUserLoginAndModernVersionAreDistinct(t *testing.T) {
	h := Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"access_token":"jwt","user":{"id":9,"username":"admin","role":"admin","status":"active"}}}`))
	}))
	for _, version := range []string{"", "3", "4"} {
		r := httptest.NewRequest("POST", "/api/user/login", nil)
		r.Header.Set(VersionHeader, version)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if version == "4" {
			if w.Code != 400 {
				t.Fatal("invalid version accepted")
			}
			continue
		}
		var response struct{ Data map[string]json.RawMessage }
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if version == "" && (string(response.Data["role"]) != "10" || string(response.Data["status"]) != "1") {
			t.Fatalf("legacy login=%s", w.Body)
		}
		if version == "3" && response.Data["role"] != nil {
			t.Fatal("v3 session was flattened")
		}
		if response.Data["user"] == nil || response.Data["access_token"] == nil {
			t.Fatal("session extension missing")
		}
	}
}

func TestDefaultKeyOutputRetainsLegacyRestrictionsAndPagination(t *testing.T) {
	h := Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"page":2,"page_size":1,"total":2,"items":[{"id":5,"user_id":9,"name":"key","status":"active","group":null,"key_prefix":"sk-abcd","budget_limited":true,"budget_micro_credits":123,"max_marketplace_multiplier_ppm":1234567,"allowed_models":[],"allowed_cidrs":["10.0.0.0/8"],"expires_at":null,"created_at":"2026-09-30T00:00:00Z"}]}}`))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/token/?p=2", nil))
	var envelope struct {
		Data struct {
			Page  int
			Total int
			Items []map[string]json.RawMessage
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	item := envelope.Data.Items[0]
	if envelope.Data.Page != 2 || envelope.Data.Total != 2 || string(item["remain_quota"]) != "61" || string(item["model_limits_enabled"]) != "true" || string(item["marketplace_multiplier_limit"]) != "1.234567" || string(item["group"]) != `""` || item["budget_limited"] != nil {
		t.Fatalf("legacy output=%s", w.Body)
	}
}

func TestLegacyCannotEraseUnrepresentableDenyAllCIDR(t *testing.T) {
	h := Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":1,"status":"active","allowed_cidrs":[]}}`))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/token/1", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "API version 3") {
		t.Fatalf("deny-all silently weakened: %d %s", w.Code, w.Body)
	}
}
