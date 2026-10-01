//go:build pgintegration

package identity

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestImportedOAuthProviderResolvesEncryptedConfigAndBinding(t *testing.T) {
	pool, _ := testDeps(t)
	var allowed atomic.Bool
	allowed.Store(true)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			user, secret, ok := r.BasicAuth()
			if !ok || user != "kept-client" || secret != "kept-secret" {
				t.Error("imported Basic auth not applied")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("code_verifier") == "" || r.Form.Get("client_secret") != "" || r.Form.Get("redirect_uri") != "http://localhost/oauth/imported" {
				t.Error("imported token request mismatch")
			}
			_, _ = w.Write([]byte(`{"access_token":"granted"}`))
			return
		}
		if allowed.Load() {
			_, _ = w.Write([]byte(`{"profile":{"subject":"kept-subject","name":"Kept Name","level":2}}`))
		} else {
			_, _ = w.Write([]byte(`{"profile":{"subject":"kept-subject","level":0}}`))
		}
	}))
	defer upstream.Close()
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), PublicURL: "http://localhost"}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	seedKey(t, pool, 7, 11)
	encrypted, err := c.encryptKey("kept-secret")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `INSERT INTO v3_identity.oauth_providers(id,name,slug,enabled,client_id,secret_ciphertext,authorization_endpoint,token_endpoint,user_info_endpoint,user_id_field,display_name_field,auth_style,access_policy)
	 VALUES (41,'Imported','imported',true,'kept-client',$1,$2,$3,$4,'profile.subject','profile.name',2,'{"conditions":[{"field":"profile.level","op":"gte","value":1}]}')`, encrypted, upstream.URL+"/authorize", upstream.URL+"/token", upstream.URL+"/userinfo")
	mustExec(t, pool, `INSERT INTO v3_identity.user_identities(provider,subject,user_id,legacy_binding_id) VALUES ('imported','kept-subject',7,42)`)
	providers, err := c.OAuthProviders(ctx)
	if err != nil || len(providers) != 1 || providers[0].Slug != "imported" {
		t.Fatalf("provider discovery=%+v %v", providers, err)
	}
	target, state, err := c.BeginOAuth(ctx, "imported", nil)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(target)
	if parsed.Query().Get("code_challenge") == "" || parsed.Query().Get("redirect_uri") != "http://localhost/oauth/imported" {
		t.Fatalf("authorization=%s", target)
	}
	u, err := c.FinishOAuth(ctx, "imported", state, "code")
	if err != nil || u.ID != 7 {
		t.Fatalf("imported binding=%+v %v", u, err)
	}
	allowed.Store(false)
	_, state, err = c.BeginOAuth(ctx, "imported", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.FinishOAuth(ctx, "imported", state, "code"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("imported access policy ignored: %v", err)
	}
	mustExec(t, pool, `UPDATE v3_identity.oauth_providers SET enabled=false WHERE id=41`)
	if _, _, err = c.BeginOAuth(ctx, "imported", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled provider admitted: %v", err)
	}
}
