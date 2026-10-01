package auxiliary

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func vertexServiceAccount(t *testing.T, tokenURL string) (string, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := json.Marshal(map[string]string{"project_id": "project", "client_email": "account@example.test",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "token_uri": tokenURL})
	if err != nil {
		t.Fatal(err)
	}
	return string(secret), &key.PublicKey
}

func TestVertexAuxiliaryServiceAccountUsesSelectedClientForOAuthAndNativeHTTP(t *testing.T) {
	var tokenCalls, nativeCalls, transportCalls, selections atomic.Int64
	var publicKey *rsa.PublicKey
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Selected-Client") != "stable-credential" {
			t.Error("selected transport bypassed")
		}
		switch r.URL.Path {
		case "/oauth":
			tokenCalls.Add(1)
			if r.Header.Get("Authorization") != "" || r.ParseForm() != nil || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Error("invalid OAuth exchange")
			}
			parsed, err := jwt.Parse(r.Form.Get("assertion"), func(*jwt.Token) (any, error) { return publicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience(server.URL+"/oauth"))
			if err != nil || parsed == nil || !parsed.Valid {
				t.Errorf("invalid service-account assertion: %v", err)
			}
			_, _ = io.WriteString(w, `{"access_token":"selected-token","token_type":"Bearer","expires_in":3600}`)
		case "/v1/projects/project/locations/us-east5/publishers/google/models/gemini-embedding-001:batchEmbedContents":
			nativeCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer selected-token" || r.Header.Get("X-Goog-User-Project") != "project" {
				t.Error("native authentication lost")
			}
			data, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(data), `"text":"hello"`) || !strings.Contains(string(data), "models/gemini-embedding-001") {
				t.Errorf("native body=%s", data)
			}
			_, _ = io.WriteString(w, `{"embeddings":[{"values":[1,2]}],"usageMetadata":{"promptTokenCount":7}}`)
		default:
			t.Errorf("unexpected native route: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	secret, key := vertexServiceAccount(t, server.URL+"/oauth")
	publicKey = key
	h, plan, settle, _ := testHandler(t, server.URL)
	target := &plan.targets[0]
	target.Provider, target.Secret, target.UpstreamModel, target.CredentialID = "vertex", secret, "gemini-embedding-001", 91
	target.ProxyURL = "http://unused-proxy.invalid:8080"
	target.Settings = map[string]any{"api_version": `{"alias":"us-east5"}`}
	target.Fingerprint = gateway.CredentialFingerprint{UserAgent: "credential-agent", TLSProfile: "custom-profile"}
	originalTarget := *target
	selected := &http.Client{Timeout: 2 * time.Minute, Transport: vectorRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls.Add(1)
		clone := r.Clone(r.Context())
		clone.Header.Set("X-Selected-Client", "stable-credential")
		return server.Client().Transport.RoundTrip(clone)
	})}
	h.cfg.Clients = func(_ context.Context, candidate gateway.Target) (*http.Client, error) {
		selections.Add(1)
		if !reflect.DeepEqual(candidate, originalTarget) {
			t.Error("selector received rewritten credential identity")
		}
		return selected, nil
	}
	for i := 0; i < 2; i++ {
		w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"hello"}`)
		if w.Code != 200 || !settle.out.Charge || settle.out.Usage.PromptTokens != 7 || settle.out.Usage.Estimated {
			t.Fatalf("status=%d outcome=%+v body=%s", w.Code, settle.out, w.Body.String())
		}
	}
	if selections.Load() != 2 || tokenCalls.Load() != 1 || nativeCalls.Load() != 2 || transportCalls.Load() != 3 || selected.Timeout != 2*time.Minute || selected.CheckRedirect != nil {
		t.Fatalf("selections=%d oauth=%d native=%d transport=%d selected client mutated=%v", selections.Load(), tokenCalls.Load(), nativeCalls.Load(), transportCalls.Load(), selected)
	}
}

func TestVertexAuxiliarySelectedClientFailuresSendNoOAuthOrNativeHTTP(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	secret, _ := vertexServiceAccount(t, server.URL+"/oauth")
	for _, test := range []struct {
		name        string
		client      *http.Client
		err         error
		directToken bool
	}{
		{"factory error", nil, errors.New("private selection details"), false}, {"nil client", nil, nil, false}, {"custom TLS without transport", &http.Client{}, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, plan, settle, limits := testHandler(t, server.URL)
			plan.targets[0].Provider, plan.targets[0].Secret, plan.targets[0].UpstreamModel = "vertex", secret, "gemini-embedding-001"
			if test.directToken {
				plan.targets[0].Secret = `{"project_id":"project","access_token":"direct-token"}`
			}
			plan.targets[0].Fingerprint.TLSProfile = "custom-profile"
			h.cfg.Clients = func(context.Context, gateway.Target) (*http.Client, error) { return test.client, test.err }
			w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"hello"}`)
			if w.Code != 502 || calls.Load() != 0 || !settle.finalized || settle.out.Charge || limits.acquired != limits.released || strings.Contains(w.Body.String(), "private selection details") {
				t.Fatalf("status=%d calls=%d outcome=%+v body=%s", w.Code, calls.Load(), settle.out, w.Body.String())
			}
		})
	}
}
