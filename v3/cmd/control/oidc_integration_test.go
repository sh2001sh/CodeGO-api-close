//go:build pgintegration

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/identity/oidc"
)

func testOIDCConfig(t *testing.T) oidc.Config {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return oidc.Config{Issuer: "http://localhost:3002", ClientID: "nodebb", ClientSecret: strings.Repeat("c", 32), RedirectURI: "http://localhost:4567/auth/codego/callback", PrivateKey: key}
}

func verifyOIDCAssembly(t *testing.T, h http.Handler, cfg oidc.Config, session string) {
	t.Helper()
	call := func(method, path, token, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://control.test"+path, strings.NewReader(body))
		r.Header.Set("Origin", "http://localhost:4567")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("OIDC %s %s: %d want %d: %s", method, path, w.Code, status, w.Body)
		}
		return w
	}
	call("GET", "/.well-known/openid-configuration", "", "", 200)
	call("GET", "/api/oidc/jwks", "", "", 200)
	w := call("GET", "/api/oidc/authorize", "", "", 400)
	var protocolError struct {
		Error   string `json:"error"`
		Success *bool  `json:"success"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &protocolError); err != nil || protocolError.Error != "invalid_request" || protocolError.Success != nil {
		t.Fatalf("parameter failure lost OAuth error envelope: %v %s", err, w.Body)
	}
	call("POST", "/api/oidc/token", "", "grant_type=authorization_code", 401)
	verifier := strings.Repeat("v", 43)
	digest := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {cfg.ClientID}, "redirect_uri": {cfg.RedirectURI}, "response_type": {"code"}, "state": {"assembly-state"}, "scope": {"openid profile email"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}}
	w = call("GET", "/api/oidc/authorize?"+q.Encode(), session, "", 302)
	callback, err := url.Parse(w.Header().Get("Location"))
	if err != nil || callback.Query().Get("code") == "" || callback.Query().Get("state") != "assembly-state" {
		t.Fatalf("OIDC callback: %v %s", err, w.Header().Get("Location"))
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {callback.Query().Get("code")}, "redirect_uri": {cfg.RedirectURI}, "code_verifier": {verifier}, "client_id": {cfg.ClientID}, "client_secret": {cfg.ClientSecret}}
	w = call("POST", "/api/oidc/token", "", form.Encode(), 200)
	var tokens struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil || tokens.AccessToken == "" || tokens.IDToken == "" {
		t.Fatalf("OIDC tokens: %v %s", err, w.Body)
	}
	call("POST", "/api/oidc/token", "", form.Encode(), 400)
	call("GET", "/api/oidc/userinfo", tokens.AccessToken, "", 200)
	call("POST", "/api/oidc/userinfo", tokens.AccessToken, "", 200)
	call("POST", "/api/oidc/userinfo", session, "", 401)
}
