//go:build pgintegration

package identity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func passkeyRequest(c *Control, path, token string, body []byte, cookies []*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "https://codego.test"+path, bytes.NewReader(body))
	r.Header.Set("Origin", "https://codego.test")
	r.RemoteAddr = "127.0.0.1:1000"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, r)
	return w
}
func passkeyChallenge(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("begin status=%d body=%s", w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			PublicKey struct {
				Challenge string `json:"challenge"`
			} `json:"publicKey"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.PublicKey.Challenge == "" {
		t.Fatalf("missing challenge: %s", w.Body.String())
	}
	return result.Data.PublicKey.Challenge
}

func TestPasskeyRealSignatureRegistrationLoginAndReplay(t *testing.T) {
	pool, _ := testDeps(t)
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), PublicURL: "https://codego.test"}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Register(ctx, RegisterInput{Username: "passkey_user", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.NewSession(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	virtual := newVirtualPasskey(t)
	begin := passkeyRequest(c, "/api/passkey/register/begin", s.AccessToken, nil, nil)
	challenge := passkeyChallenge(t, begin)
	registered := passkeyRequest(c, "/api/passkey/register/finish", s.AccessToken, virtual.registerResponse(t, challenge), begin.Result().Cookies())
	if registered.Code != 200 {
		t.Fatalf("registration status=%d body=%s", registered.Code, registered.Body.String())
	}
	loginBegin := passkeyRequest(c, "/api/passkey/login/begin", "", nil, nil)
	challenge = passkeyChallenge(t, loginBegin)
	body := virtual.loginResponse(t, challenge, []byte(strconv.FormatInt(u.ID, 10)), 1)
	login := passkeyRequest(c, "/api/passkey/login/finish", "", body, loginBegin.Result().Cookies())
	if login.Code != 200 {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	var result struct {
		Data Session `json:"data"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.User.ID != u.ID || result.Data.AccessToken == "" {
		t.Fatalf("wrong login principal: %+v", result.Data.User)
	}
	replay := passkeyRequest(c, "/api/passkey/login/finish", "", body, loginBegin.Result().Cookies())
	if replay.Code == 200 {
		t.Fatal("consumed ceremony accepted again")
	}
	// A new, correctly signed assertion with an unchanged nonzero counter is
	// rejected as a clone, not allowed to overwrite the stored counter.
	cloneBegin := passkeyRequest(c, "/api/passkey/login/begin", "", nil, nil)
	challenge = passkeyChallenge(t, cloneBegin)
	clone := passkeyRequest(c, "/api/passkey/login/finish", "", virtual.loginResponse(t, challenge, []byte(strconv.FormatInt(u.ID, 10)), 1), cloneBegin.Result().Cookies())
	if clone.Code != http.StatusUnauthorized {
		t.Fatalf("clone status=%d body=%s", clone.Code, clone.Body.String())
	}
	verifyBegin := passkeyRequest(c, "/api/user/passkey/verify/begin", s.AccessToken, nil, nil)
	challenge = passkeyChallenge(t, verifyBegin)
	verified := passkeyRequest(c, "/api/user/passkey/verify/finish", s.AccessToken, virtual.loginResponse(t, challenge, []byte(strconv.FormatInt(u.ID, 10)), 2), verifyBegin.Result().Cookies())
	if verified.Code != 200 {
		t.Fatalf("verification status=%d body=%s", verified.Code, verified.Body.String())
	}
	deleteRequest := httptest.NewRequest(http.MethodDelete, "https://codego.test/api/user/passkey", nil)
	deleteRequest.Header.Set("Authorization", "Bearer "+s.AccessToken)
	deleteRequest.Header.Set("Origin", "https://codego.test")
	for _, cookie := range verified.Result().Cookies() {
		deleteRequest.AddCookie(cookie)
	}
	deleted := httptest.NewRecorder()
	c.Handler().ServeHTTP(deleted, deleteRequest)
	if deleted.Code != 200 {
		t.Fatalf("verified deletion status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.passkeys WHERE user_id=$1`, u.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("remaining credentials=%d err=%v", remaining, err)
	}
}
