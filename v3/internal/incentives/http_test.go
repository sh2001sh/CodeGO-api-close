package incentives

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIncentiveHTTPRejectsUnauthorizedAdminCrossOriginAndMalformedInput(t *testing.T) {
	for _, c := range []struct {
		method, path, body, origin, role string
		want                             int
	}{
		{"GET", "/api/user/aff/rewards", "", "", "", 401},
		{"GET", "/api/subscription/admin/referral-policy", "", "", "user", 403},
		{"GET", "/api/subscription/admin/referral-policy", "", "", "admin", 403},
		{"PUT", "/api/subscription/admin/referral-policy", "{}", "https://outside.test", "root", 403},
		{"PUT", "/api/subscription/admin/referral-policy", "{", "", "root", 400},
		{"GET", "/api/subscription/admin/referral-qualifications?page=nope", "", "", "root", 400},
		{"POST", "/api/subscription/admin/referral-qualifications/bad/approve", "", "", "root", 400},
		{"GET", "/api/daily-lucky-number/self", "", "", "", 404},
		{"GET", "/api/daily-lucky-number/history", "", "", "user", 404},
		{"PUT", "/api/daily-lucky-number/admin/config", "{}", "", "root", 404},
		{"POST", "/api/daily-lucky-number/admin/backfill", "{}", "", "root", 404},
	} {
		mux := http.NewServeMux()
		s := New(nil, nil, Config{})
		s.Register(mux, func(*http.Request) (Actor, error) {
			if c.role == "" {
				return Actor{}, errors.New("no session")
			}
			return Actor{UserID: 1, Role: c.role}, nil
		})
		r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		r.Header.Set("Origin", c.origin)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Fatalf("%+v got %d %s", c, w.Code, w.Body.String())
		}
	}
}
func TestPublicRulesNeverExposeOperationalCosts(t *testing.T) {
	raw, err := json.Marshal(publicRules(defaultSettings()))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"cost_per_usd", "monthly_budget_usd"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("private rule leaked: %s", raw)
		}
	}
}
