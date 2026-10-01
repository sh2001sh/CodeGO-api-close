//go:build pgintegration

package identity

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestOAuthPKCESingleUseAndExplicitAccountBinding(t *testing.T) {
	pool, _ := testDeps(t)
	verifiers := make(chan string, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			verifiers <- r.Form.Get("code_verifier")
			if r.Form.Get("code") != "valid-code" {
				w.WriteHeader(400)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"test-access"}`))
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer test-access" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"sub":"external-subject","name":"OAuth user"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), OAuth: map[string]OAuthProvider{"test": {ClientID: "test-client", ClientSecret: "test-secret", AuthorizationURL: upstream.URL + "/authorize", TokenURL: upstream.URL + "/token", UserInfoURL: upstream.URL + "/userinfo", RedirectURL: "http://localhost/api/oauth/test/callback"}}}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	target, state, err := c.BeginOAuth(ctx, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.FinishOAuth(ctx, "test", state, "valid-code")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(target)
	challenge := sha256.Sum256([]byte(<-verifiers))
	if parsed.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(challenge[:]) || parsed.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("PKCE verifier does not match authorization challenge")
	}
	if _, err := c.FinishOAuth(ctx, "test", state, "valid-code"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("replayed OAuth state accepted: %v", err)
	}
	_, state, err = c.BeginOAuth(ctx, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.FinishOAuth(ctx, "test", state, "valid-code")
	if err != nil || second.ID != u.ID {
		t.Fatalf("repeat login created another user: %+v %v", second, err)
	}
	bob, err := c.Register(ctx, RegisterInput{Username: "bob_binding", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	_, state, err = c.BeginOAuth(ctx, "test", &bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FinishOAuth(ctx, "test", state, "valid-code"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("identity stolen by binding: %v", err)
	}
	returnTo := "/api/oidc/authorize?client_id=nodebb&scope=openid"
	beginRequest := httptest.NewRequest(http.MethodGet, "https://codego.test/api/oauth/test?returnTo="+url.QueryEscape(returnTo), nil)
	begin := httptest.NewRecorder()
	c.Handler().ServeHTTP(begin, beginRequest)
	if begin.Code != http.StatusFound {
		t.Fatalf("browser begin status=%d body=%s", begin.Code, begin.Body.String())
	}
	location, err := url.Parse(begin.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	callback := httptest.NewRequest(http.MethodGet, "https://codego.test/api/oauth/test?state="+url.QueryEscape(location.Query().Get("state"))+"&code=valid-code", nil)
	callback.Header.Set("Accept", "text/html")
	for _, cookie := range begin.Result().Cookies() {
		callback.AddCookie(cookie)
	}
	finish := httptest.NewRecorder()
	c.Handler().ServeHTTP(finish, callback)
	if finish.Code != http.StatusSeeOther || finish.Header().Get("Location") != returnTo {
		t.Fatalf("browser callback did not reach app: status=%d location=%q body=%s", finish.Code, finish.Header().Get("Location"), finish.Body.String())
	}
	begin = httptest.NewRecorder()
	c.Handler().ServeHTTP(begin, httptest.NewRequest(http.MethodGet, "https://codego.test/api/oauth/test?returnTo=%2F%2Fevil.test", nil))
	location, _ = url.Parse(begin.Header().Get("Location"))
	callback = httptest.NewRequest(http.MethodGet, "https://codego.test/api/oauth/test?state="+url.QueryEscape(location.Query().Get("state"))+"&code=valid-code", nil)
	callback.Header.Set("Accept", "text/html")
	for _, cookie := range begin.Result().Cookies() {
		if cookie.Name == "codego_oauth_state" {
			callback.AddCookie(cookie)
		}
	}
	callback.AddCookie(&http.Cookie{Name: "codego_oauth_return", Value: url.QueryEscape("//evil.test")})
	finish = httptest.NewRecorder()
	c.Handler().ServeHTTP(finish, callback)
	if finish.Code != http.StatusSeeOther || finish.Header().Get("Location") != "/dashboard" {
		t.Fatalf("forged return cookie escaped origin: status=%d location=%q", finish.Code, finish.Header().Get("Location"))
	}
}
