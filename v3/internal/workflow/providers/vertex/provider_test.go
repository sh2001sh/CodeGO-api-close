package vertex

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestServiceAccountSubmitPollInlineContent(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const operation = "projects/fixture-project/locations/us-central1/publishers/google/models/veo-3.1-generate-preview/operations/task-id"
	var exchanges atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			exchanges.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Error("wrong OAuth grant")
			}
			parts := strings.Split(r.Form.Get("assertion"), ".")
			if len(parts) != 3 {
				t.Error("invalid assertion")
				w.WriteHeader(400)
				return
			}
			signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
			hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, hash[:], signature); err != nil {
				t.Error(err)
			}
			payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
			var claims map[string]any
			_ = json.Unmarshal(payload, &claims)
			if claims["aud"] != server.URL+"/token" || claims["iss"] != "fixture@example.test" || claims["scope"] != "https://www.googleapis.com/auth/cloud-platform" {
				t.Errorf("claims %+v", claims)
			}
			_, _ = io.WriteString(w, `{"access_token":"fixture-access","expires_in":3600,"token_type":"Bearer"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-access" || r.Header.Get("X-Goog-User-Project") != "fixture-project" || r.Method != http.MethodPost {
			t.Errorf("wrong method or auth")
		}
		switch r.URL.Path {
		case "/v1/projects/fixture-project/locations/europe-west4/publishers/google/models/veo-3.1-generate-preview:predictLongRunning":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["parameters"].(map[string]any)["durationSeconds"] != float64(8) {
				t.Error("duration conversion failed")
			}
			_, _ = io.WriteString(w, `{"name":"`+operation+`"}`)
		case "/v1/projects/fixture-project/locations/us-central1/publishers/google/models/veo-3.1-generate-preview:fetchPredictOperation":
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["operationName"] != operation {
				t.Errorf("operation name %q", payload["operationName"])
			}
			_, _ = io.WriteString(w, `{"name":"`+operation+`","done":true,"response":{"durationSeconds":8,"videos":[{"bytesBase64Encoded":"dmlkZW8=","mimeType":"video/mp4"}]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	pkcs, _ := x509.MarshalPKCS8PrivateKey(key)
	credentials, _ := json.Marshal(map[string]string{"project_id": "fixture-project", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs})), "private_key_id": "key-id", "client_email": "fixture@example.test", "token_uri": server.URL + "/token", "region": "europe-west4"})
	target := gateway.Target{BaseURL: server.URL + "/v1", Secret: string(credentials), UpstreamModel: "veo-3.1-generate-preview"}
	p := New(server.Client())
	r, err := p.Submit(context.Background(), target, native.Submit{Model: "alias", Body: []byte(`{"prompt":"cat","seconds":"8"}`)})
	if err != nil || r.ID != operation || r.Status != "queued" {
		t.Fatalf("submit %+v %v", r, err)
	}
	task := native.Task{UpstreamID: r.ID, Model: "veo-3.1-generate-preview", Data: r.Data}
	r, err = p.Poll(context.Background(), target, task)
	if err != nil || r.Status != "completed" || r.Units != 8 {
		t.Fatalf("poll %+v %v", r, err)
	}
	task.Data = r.Data
	resp, err := p.Content(context.Background(), target, task)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "video" || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Errorf("inline content %q", body)
	}
	if exchanges.Load() != 1 {
		t.Errorf("OAuth exchanges %d, want cached one", exchanges.Load())
	}
}

func TestExpressKeyAndFetchOperation(t *testing.T) {
	const operation = "publishers/google/models/veo-3.1-generate-preview/operations/task-id"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "fixture-key" || r.Header.Get("Authorization") != "" {
			t.Error("wrong express authentication")
		}
		switch r.URL.Path {
		case "/v1/publishers/google/models/veo-3.1-generate-preview:predictLongRunning":
			_, _ = io.WriteString(w, `{"name":"`+operation+`"}`)
		case "/v1/publishers/google/models/veo-3.1-generate-preview:fetchPredictOperation":
			_, _ = io.WriteString(w, `{"name":"`+operation+`","done":false}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := New(server.Client())
	target := gateway.Target{BaseURL: server.URL, Secret: "fixture-key"}
	r, err := p.Submit(context.Background(), target, native.Submit{Model: "veo-3.1-generate-preview", Body: []byte(`{"prompt":"cat"}`)})
	if err != nil {
		t.Fatal(err)
	}
	r, err = p.Poll(context.Background(), target, native.Task{UpstreamID: r.ID})
	if err != nil || r.Status != "in_progress" {
		t.Fatalf("poll %+v %v", r, err)
	}
}

func TestPreflightAndProviderErrors(t *testing.T) {
	_, err := New(nil).Submit(context.Background(), gateway.Target{Secret: `{"project_id":"fixture"}`}, native.Submit{Model: "veo", Body: []byte(`{"prompt":"cat"}`)})
	var invalid *native.InvalidRequest
	if !errors.As(err, &invalid) {
		t.Fatalf("wrong credential failure %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = io.WriteString(w, `{"error":{"message":"denied"}}`)
	}))
	defer server.Close()
	_, err = New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "fixture-key"}, native.Submit{Model: "veo", Body: []byte(`{"prompt":"cat"}`)})
	var rejected *native.Rejected
	if !errors.As(err, &rejected) || rejected.Status != 403 {
		t.Fatalf("wrong provider failure %v", err)
	}
	_, err = New(nil).Poll(context.Background(), gateway.Target{}, native.Task{UpstreamID: "projects/x/locations/global/publishers/google/models/veo/operations/../id"})
	if err == nil {
		t.Fatal("accepted traversal operation")
	}
}
