//go:build pgintegration

package identity

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLinuxDOExistingBindingUsesLegacySubjectNamespace(t *testing.T) {
	pool, _ := testDeps(t)
	seedKey(t, pool, 7, 11)
	mustExec(t, pool, `INSERT INTO v3_identity.user_identities(provider,subject,user_id) VALUES ('linux_do','kept-user',7)`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"access_token":"test-access"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"kept-user","name":"Existing User","trust_level":2}`))
	}))
	defer upstream.Close()
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32),
		OAuth: map[string]OAuthProvider{"linuxdo": {ClientID: "test-client", ClientSecret: "test-secret", AuthorizationURL: upstream.URL + "/auth", TokenURL: upstream.URL + "/token", UserInfoURL: upstream.URL + "/userinfo", RedirectURL: "http://localhost/oauth/linuxdo", UserIDField: "id"}}}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	_, state, err := c.BeginOAuth(ctx, "linuxdo", nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.FinishOAuth(ctx, "linuxdo", state, "code")
	if err != nil || u.ID != 7 {
		t.Fatalf("existing LinuxDO binding lost: user=%d error=%v", u.ID, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.user_identities`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate identity created: %d %v", count, err)
	}
}
