//go:build pgintegration

package legacy

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

// Exercise the actual v3 WebAuthn ceremony using a v2-encoded migrated COSE
// credential, rather than only inspecting stored bytes.
func assertHistoryPasskeyLogin(t *testing.T, pool *pgxpool.Pool, private ed25519.PrivateKey) {
	t.Helper()
	c, err := identity.NewControl(pool, identity.ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: make([]byte, 32), PublicURL: "https://codego.test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	request := func(path string, body []byte, cookies []*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "https://codego.test"+path, bytes.NewReader(body))
		r.Header.Set("Origin", "https://codego.test")
		r.Header.Set("Content-Type", "application/json")
		r.RemoteAddr = "127.0.0.1:1234"
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, r)
		return w
	}
	begin := request("/api/passkey/login/begin", nil, nil)
	if begin.Code != 200 {
		t.Fatalf("imported passkey login begin status=%d body=%s", begin.Code, begin.Body.String())
	}
	var envelope struct {
		Data struct {
			PublicKey struct {
				Challenge string `json:"challenge"`
			} `json:"publicKey"`
		} `json:"data"`
	}
	if err = json.Unmarshal(begin.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	challenge := envelope.Data.PublicKey.Challenge
	if challenge == "" {
		t.Fatal("login challenge missing")
	}
	client := []byte(`{"type":"webauthn.get","challenge":"` + challenge + `","origin":"https://codego.test"}`)
	rp := sha256.Sum256([]byte("codego.test"))
	data := append(append([]byte{}, rp[:]...), 0x1d) // UP, UV, backup eligible/state retained.
	data = binary.BigEndian.AppendUint32(data, 18)
	hash := sha256.Sum256(client)
	signed := append(append([]byte{}, data...), hash[:]...)
	b64 := base64.RawURLEncoding.EncodeToString
	id := []byte("kept-credential")
	body, err := json.Marshal(map[string]any{"id": b64(id), "rawId": b64(id), "type": "public-key", "response": map[string]any{
		"authenticatorData": b64(data), "clientDataJSON": b64(client), "signature": b64(ed25519.Sign(private, signed)), "userHandle": b64([]byte("7"))}, "clientExtensionResults": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	login := request("/api/passkey/login/finish", body, begin.Result().Cookies())
	if login.Code != 200 {
		t.Fatalf("imported credential signature rejected status=%d body=%s", login.Code, login.Body.String())
	}
	var result struct {
		Data identity.Session `json:"data"`
	}
	if err = json.Unmarshal(login.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.User.ID != 7 || result.Data.AccessToken == "" {
		t.Fatal("imported credential resolved wrong identity")
	}
	replay := request("/api/passkey/login/finish", body, begin.Result().Cookies())
	if replay.Code == 200 {
		t.Fatal("imported passkey replay accepted")
	}
}
