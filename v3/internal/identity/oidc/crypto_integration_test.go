//go:build pgintegration

package oidc

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func verifyIDTokenAndJWK(t *testing.T, s *Server, raw, subject string) {
	t.Helper()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/oidc/jwks", nil))
	var keys struct {
		Keys []map[string]string `json:"keys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(keys.Keys) != 1 {
		t.Fatalf("JWKS: %d %s", w.Code, w.Body.String())
	}
	jwk := keys.Keys[0]
	n, err := base64.RawURLEncoding.DecodeString(jwk["n"])
	if err != nil {
		t.Fatal(err)
	}
	e, err := base64.RawURLEncoding.DecodeString(jwk["e"])
	if err != nil {
		t.Fatal(err)
	}
	key := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	token, err := jwt.Parse(raw, func(token *jwt.Token) (any, error) {
		if token.Header["kid"] != jwk["kid"] || jwk["alg"] != "RS256" {
			t.Fatal("unexpected signing key")
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(s.cfg.Issuer), jwt.WithAudience(s.cfg.ClientID), jwt.WithTimeFunc(s.cfg.Now), jwt.WithIssuedAt())
	if err != nil || !token.Valid {
		t.Fatalf("signature/issuer/audience invalid: %v", err)
	}
	claims := token.Claims.(jwt.MapClaims)
	if claims["sub"] != subject || claims["nonce"] != "browser-nonce" || claims["exp"] != float64(s.cfg.Now().Add(tokenTTL).Unix()) {
		t.Fatalf("ID claims=%+v", claims)
	}
}

func testConcurrentExchange(t *testing.T, f *fixture) {
	t.Helper()
	code := f.code(t, 1, "openid")
	var wg sync.WaitGroup
	statuses := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- f.exchange(tokenValues(f.s.cfg, code), true).Code
		}()
	}
	wg.Wait()
	close(statuses)
	winners := 0
	for status := range statuses {
		if status == 200 {
			winners++
		} else if status != 400 {
			t.Fatalf("unexpected status: %d", status)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent exchange winners=%d", winners)
	}
}
