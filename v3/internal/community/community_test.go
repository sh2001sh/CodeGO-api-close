package community

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testServiceSecret = "community-test-secret-at-least-32-characters"

func TestDedicatedServiceCredentialAndClosedDefaults(t *testing.T) {
	if err := New(nil, Config{}).Authorize(testServiceSecret); !errors.Is(err, ErrDisabled) {
		t.Fatalf("missing secret: %v", err)
	}
	s := New(nil, Config{ServiceSecret: testServiceSecret})
	if err := s.Authorize("other"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong secret: %v", err)
	}
	if err := s.Authorize(testServiceSecret); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		secret     string
		credential string
		want       int
	}{
		{"", testServiceSecret, 503}, {testServiceSecret, "", 401}, {testServiceSecret, "user-api-key", 401},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/community/v1/members/ABC234", nil)
		r.Header.Set("Authorization", "Bearer "+tc.credential)
		w := httptest.NewRecorder()
		New(nil, Config{ServiceSecret: tc.secret}).Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("auth status %d want %d", w.Code, tc.want)
		}
	}
}

func TestInvalidInputsFailBeforeDatabaseAccess(t *testing.T) {
	s := New(nil, Config{ServiceSecret: testServiceSecret})
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/community/v1/members/invalid", ""},
		{"GET", "/api/community/v1/members/ABC234/channels?page=0", ""},
		{"GET", "/api/community/v1/sellers?page_size=51", ""},
		{"GET", "/api/community/v1/sellers?sort=DROP", ""},
		{"PUT", "/api/community/v1/channels/channel-1/rating", `{"viewer_sub":"ABC234","stars":0}`},
		{"PUT", "/api/community/v1/channels/channel-1/rating", `{"viewer_sub":"ABC234","stars":2.5}`},
		{"PUT", "/api/community/v1/channels/channel-1/rating", `{"viewer_sub":"ABC234","stars":5} {}`},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+testServiceSecret)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("%s: status %d body %s", tc.path, w.Code, w.Body.String())
		}
	}
}

func TestSubjectValidationAndPublicIdentityPrivacy(t *testing.T) {
	for _, input := range []string{"abc234", "ABC230", "ABC23I", "AB234", "ABC2345", "ABC@34"} {
		if _, err := normalizeSubject(input); !errors.Is(err, ErrInvalidSubject) {
			t.Fatalf("accepted %q", input)
		}
	}
	if value, err := normalizeSubject(" ABC234 "); err != nil || value != "ABC234" {
		t.Fatalf("subject %q %v", value, err)
	}
	for _, name := range []string{"user@example.com", "+86 138 1234 5678", ""} {
		u, d := publicIdentity("ABC234", name, name)
		if strings.Contains(u, "@") || strings.Contains(d, "@") || u == name || d == name {
			t.Fatalf("identity leaked %q / %q", u, d)
		}
	}
	if got := keywordPattern("A_%!"); got != "%a!_!%!!%" {
		t.Fatalf("literal search %q", got)
	}
}
