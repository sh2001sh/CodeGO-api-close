package vertex

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func testAccount(t *testing.T) (Credentials, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return Credentials{ProjectID: "test-project", PrivateKeyID: "kid-one", ClientEmail: "service@example.invalid",
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}))}, key
}

func TestServiceAccountJWTAndConcurrentRefresh(t *testing.T) {
	c, key := testAccount(t)
	var requests atomic.Int64
	var now atomic.Int64
	now.Store(time.Now().Unix())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.ParseForm() != nil || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Error("wrong token request")
			w.WriteHeader(400)
			return
		}
		token, err := jwt.Parse(r.Form.Get("assertion"), func(token *jwt.Token) (any, error) {
			if token.Header["alg"] != "RS256" || token.Header["kid"] != "kid-one" {
				t.Error("wrong JWT header")
			}
			return &key.PublicKey, nil
		}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithTimeFunc(func() time.Time { return time.Unix(now.Load(), 0) }))
		if err != nil || !token.Valid {
			t.Errorf("signature failed: %v", err)
			w.WriteHeader(400)
			return
		}
		claims := token.Claims.(jwt.MapClaims)
		if claims["aud"] != "http://"+r.Host || claims["iss"] != c.ClientEmail || claims["scope"] != "https://www.googleapis.com/auth/cloud-platform" || int64(claims["exp"].(float64))-int64(claims["iat"].(float64)) != 3600 {
			t.Errorf("invalid JWT claims: %+v", claims)
		}
		_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","token_type":"Bearer","expires_in":3600}`, requests.Load())
	}))
	defer server.Close()
	c.TokenURI = server.URL
	p := Provider{HTTPClient: server.Client(), Now: func() time.Time { return time.Unix(now.Load(), 0) }, tokens: &tokenCache{entries: make(map[[32]byte]*tokenEntry)}}
	secret := encoded(t, c)
	build := func(want string) {
		t.Helper()
		var wg sync.WaitGroup
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, err := p.BuildRequest(context.Background(), &gateway.Request{Model: "gemini", Protocol: gateway.ProtocolGemini, Body: []byte(`{"contents":[]}`)}, gateway.Target{Secret: secret})
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = out.Body.Close() }()
				if out.Header.Get("Authorization") != "Bearer "+want {
					t.Error("incorrect cached token")
				}
			}()
		}
		wg.Wait()
	}
	build("token-1")
	if requests.Load() != 1 {
		t.Fatalf("duplicate OAuth exchanges: %d", requests.Load())
	}
	now.Add(3550)
	build("token-2")
	if requests.Load() != 2 {
		t.Fatalf("expired cache did not refresh exactly once: %d", requests.Load())
	}
}

func TestRefreshFailureNeverUsesExpiredTokenOrLeaksSecrets(t *testing.T) {
	c, _ := testAccount(t)
	var requests atomic.Int64
	var now atomic.Int64
	now.Store(time.Now().Unix())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"access_token":"secret-access-token","expires_in":120}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error_description":"SECRET-SHOULD-NOT-LEAK"}`))
	}))
	defer server.Close()
	p := Provider{HTTPClient: server.Client(), TokenEndpoint: server.URL, Now: func() time.Time { return time.Unix(now.Load(), 0) }, tokens: &tokenCache{entries: make(map[[32]byte]*tokenEntry)}}
	if _, err := p.accessToken(context.Background(), encoded(t, c), c, ""); err != nil {
		t.Fatal(err)
	}
	now.Add(121)
	token, err := p.accessToken(context.Background(), encoded(t, c), c, "")
	if err == nil || token != "" || !strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "secret-access-token") {
		t.Fatalf("bad refresh failure: %q %v", token, err)
	}
	if requests.Load() != 2 {
		t.Fatal("refresh was not attempted")
	}
}

func TestTokenResponseValidation(t *testing.T) {
	c, _ := testAccount(t)
	for _, body := range []string{`{`, `{}`, `{"access_token":"a","expires_in":0}`, `{"access_token":"a","expires_in":-1}`, `{"access_token":"a","expires_in":9999999999}`, `{"access_token":"a","expires_in":3600,"token_type":"MAC"}`, `{"access_token":"a\r\nb","expires_in":3600}`, strings.Repeat("x", (1<<20)+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		p := Provider{HTTPClient: server.Client(), TokenEndpoint: server.URL}
		_, _, err := p.exchangeToken(context.Background(), c, server.URL, "")
		server.Close()
		if err == nil {
			t.Fatal("malformed token response accepted")
		}
	}
}

func TestCancellationWhileWaitingAndRefreshing(t *testing.T) {
	c, _ := testAccount(t)
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"access_token":"a","expires_in":3600}`))
	}))
	defer server.Close()
	defer close(release)
	p := Provider{HTTPClient: server.Client(), TokenEndpoint: server.URL, tokens: &tokenCache{entries: make(map[[32]byte]*tokenEntry)}}
	ctx, cancel := context.WithCancel(context.Background())
	leader := make(chan error, 1)
	go func() { _, err := p.accessToken(ctx, encoded(t, c), c, ""); leader <- err }()
	select {
	case <-started:
	case err := <-leader:
		t.Fatalf("token refresh failed before reaching server: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("token refresh never reached server")
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer waitCancel()
	if _, err := p.accessToken(waitCtx, encoded(t, c), c, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting caller did not cancel: %v", err)
	}
	cancel()
	select {
	case err := <-leader:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("refresh did not cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled token refresh did not return")
	}
}

func TestInvalidServiceAccounts(t *testing.T) {
	for _, secret := range []string{"", "{", "[]", `{"project_id":"p","private_key":"secret"}`, `{"private_key":"secret","client_email":"email"}`, `{"project_id":"p","private_key":"secret","client_email":"email"}`, `{"api_key":"secret","access_token":"secret"}`} {
		_, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Model: "gemini", Protocol: gateway.ProtocolGemini, Body: []byte(`{}`)}, gateway.Target{Secret: secret})
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("bad credential rejection: %v", err)
		}
	}
}

func TestTokenEndpointNeverFollowsRedirect(t *testing.T) {
	c, _ := testAccount(t)
	var leaked atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	p := Provider{HTTPClient: server.Client(), TokenEndpoint: server.URL}
	_, _, err := p.exchangeToken(context.Background(), c, server.URL, "")
	if err == nil || leaked.Load() {
		t.Fatalf("OAuth assertion followed a redirect: %v", err)
	}
}
