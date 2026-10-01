package oidc

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func authorizationValues(cfg Config) url.Values {
	return url.Values{"client_id": {cfg.ClientID}, "redirect_uri": {cfg.RedirectURI}, "response_type": {"code"},
		"scope": {"openid profile email"}, "state": {"browser-state"}, "nonce": {"browser-nonce"},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest(strings.Repeat("v", 43)))}}
}

func TestDiscoveryAndLoginRedirect(t *testing.T) {
	s := unitServer(t)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
	var document map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || document["issuer"] != s.cfg.Issuer || document["jwks_uri"] != s.cfg.Issuer+"/api/oidc/jwks" ||
		document["grant_types_supported"].([]any)[0] != "authorization_code" || document["code_challenge_methods_supported"].([]any)[0] != "S256" {
		t.Fatalf("incorrect discovery: %s", w.Body.String())
	}
	q := authorizationValues(s.cfg)
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://evil.test/api/oidc/authorize?"+q.Encode(), nil)
	s.Handler().ServeHTTP(w, r)
	login, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != 302 || login.Host != "" || login.Path != "/sign-in" || login.Query().Get("returnTo") != "/api/oidc/authorize?"+q.Encode() {
		t.Fatalf("unsafe login redirect: %q", login)
	}
}

func TestAuthorizeInputRejectsOpenRedirectAndAmbiguity(t *testing.T) {
	s := unitServer(t)
	for _, tc := range []struct {
		name   string
		modify func(url.Values)
		status int
		code   string
	}{
		{"unknown redirect", func(q url.Values) { q.Set("redirect_uri", "https://evil.test") }, 400, "invalid_request"},
		{"unknown client", func(q url.Values) { q.Set("client_id", "evil") }, 400, "invalid_request"},
		{"duplicate redirect", func(q url.Values) { q.Add("redirect_uri", "https://evil.test") }, 400, "invalid_request"},
		{"unsupported scope", func(q url.Values) { q.Set("scope", "openid admin") }, 302, "invalid_scope"},
		{"no openid scope", func(q url.Values) { q.Set("scope", "profile") }, 302, "invalid_scope"},
		{"plain PKCE", func(q url.Values) { q.Set("code_challenge_method", "plain") }, 302, "invalid_request"},
		{"malformed PKCE", func(q url.Values) { q.Set("code_challenge", strings.Repeat("~", 43)) }, 302, "invalid_request"},
		{"oversized state", func(q url.Values) { q.Set("state", strings.Repeat("x", 513)) }, 302, "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := authorizationValues(s.cfg)
			tc.modify(q)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/oidc/authorize?"+q.Encode(), nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Code == 302 {
				u, _ := url.Parse(w.Header().Get("Location"))
				if u.Host != "community.test" || u.Query().Get("error") != tc.code {
					t.Fatalf("redirect=%q", u)
				}
			} else if !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("error=%s", w.Body.String())
			}
		})
	}
}

func TestTokenBodyAndCredentialsBoundaries(t *testing.T) {
	s := unitServer(t)
	for _, tc := range []struct {
		name, body, contentType, auth string
		status                        int
		code                          string
	}{
		{"wrong media", "{}", "application/json", "", 400, "invalid_request"},
		{"oversized", "x=" + strings.Repeat("x", 8200), "application/x-www-form-urlencoded", "", 400, "invalid_request"},
		{"duplicate client", "client_id=nodebb&client_id=nodebb", "application/x-www-form-urlencoded", "", 400, "invalid_request"},
		{"wrong secret", "client_id=nodebb&client_secret=bad&grant_type=authorization_code", "application/x-www-form-urlencoded", "", 401, "invalid_client"},
		{"invalid basic", "grant_type=authorization_code", "application/x-www-form-urlencoded", "Basic invalid", 401, "invalid_client"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/oidc/token", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			r.Header.Set("Authorization", tc.auth)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestUserClaimScopeAndVerification(t *testing.T) {
	u := user{Subject: "ABC234", Username: "alice", Email: "alice@example.test"}
	claims := userClaims(u, "openid")
	if len(claims) != 1 {
		t.Fatalf("scope leaked profile/email: %+v", claims)
	}
	claims = userClaims(u, "openid email")
	if claims["email_verified"] != false || claims["email"] != u.Email {
		t.Fatalf("unverified email: %+v", claims)
	}
	u.EmailVerified = true
	if userClaims(u, "openid profile email")["email_verified"] != true {
		t.Fatal("verified fact omitted")
	}
}
