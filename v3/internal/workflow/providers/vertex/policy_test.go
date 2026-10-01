package vertex

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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type policyTransportFunc func(*http.Request) (*http.Response, error)

func (f policyTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type policyTraceKey struct{}

func TestFrozenRequestPolicyAndCredentialClientAcrossOAuthAndVideo(t *testing.T) {
	const model = "veo-3.1-generate-preview"
	const operation = "projects/fixture-project/locations/global/publishers/google/models/" + model + "/operations/policy-task"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var wireCalls, clientSelections, tokenCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "stable-credential-client" {
			t.Error("configured credential transport bypassed")
		}
		if r.URL.Path == "/oauth" {
			tokenCalls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if len(r.Form) != 2 || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || len(strings.Split(r.Form.Get("assertion"), ".")) != 3 {
				t.Errorf("OAuth form changed by body policy: %v", r.Form)
			}
			if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Header.Get("X-Frozen") != "" || r.Header.Get("X-Policy") != "" {
				t.Error("video overrides were applied to OAuth exchange")
			}
			_, _ = io.WriteString(w, `{"access_token":"policy-access","expires_in":3600,"token_type":"Bearer"}`)
			return
		}
		if r.Header.Get("X-Frozen") != "original-header" || r.Header.Get("X-Policy") != "applied" {
			t.Error("final native headers lost original request policy")
		}
		if r.URL.Path == "/content" {
			if r.Method != http.MethodGet || r.ContentLength > 0 {
				t.Error("body parameters were applied to content GET")
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "stable-video")
			return
		}
		if r.Header.Get("Authorization") != "Bearer policy-access" {
			t.Error("OAuth bearer missing from video request")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		params, ok := body["parameters"].(map[string]any)
		if !ok || params["durationSeconds"] != float64(4) {
			t.Errorf("override did not affect converted bytes: %+v", body)
		}
		if r.URL.Path == "/v1/projects/fixture-project/locations/global/publishers/google/models/"+model+":predictLongRunning" {
			instances, ok := body["instances"].([]any)
			if !ok || len(instances) != 1 || instances[0].(map[string]any)["prompt"] != "configured prompt" {
				t.Error("prompt override was applied before conversion or original context was lost")
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"name":"`+operation+`"}`)
			return
		}
		if r.URL.Path == "/v1/projects/fixture-project/locations/global/publishers/google/models/"+model+":fetchPredictOperation" {
			if body["operationName"] != operation {
				t.Errorf("poll operation overwritten: %+v", body)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": operation, "done": true, "response": map[string]any{"videos": []any{map[string]any{"uri": server.URL + "/content", "durationSeconds": 4}}}})
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(404)
	}))
	defer server.Close()
	encodedKey, _ := x509.MarshalPKCS8PrivateKey(key)
	secret, _ := json.Marshal(map[string]string{"project_id": "fixture-project", "client_email": "policy@example.test", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})), "token_uri": server.URL + "/oauth"})
	target := gateway.Target{ChannelID: 17, CredentialID: 19, Provider: "vertex", BaseURL: server.URL, Secret: string(secret), UpstreamModel: model, Group: "selected-group",
		HeaderOverride: map[string]string{"X-Frozen": "{client_header:X-Frozen}", "X-Policy": "applied"}, StatusCodeMapping: map[string]int{"503": 200}}
	if err := json.Unmarshal([]byte(`{"operations":[{"mode":"set","path":"parameters.durationSeconds","value":4,"conditions":{"original_model":"client-veo","model":"veo-3.1-generate-preview","user_id":7,"key_id":9,"user_group":"paid","using_group":"selected-group","request_headers.x-frozen":"original-header"}},{"mode":"set","path":"instances.0.prompt","value":"configured prompt","conditions":{"original_model":"client-veo"}}]}`), &target.ParamOverride); err != nil {
		t.Fatal(err)
	}
	original := &gateway.Request{ID: "frozen-billing-request", Model: "client-veo", Body: []byte(`{"model":"client-veo","prompt":"original prompt","duration":8}`),
		Principal:      gateway.Principal{UserID: 7, KeyID: 9, Group: "paid", AllowedModels: []string{"client-veo"}},
		PricingHeaders: map[string]string{"X-Frozen": "original-header"}, ClientHeaders: map[string]string{"X-Trace": "original-trace"}}
	before, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	configured := &http.Client{Transport: policyTransportFunc(func(req *http.Request) (*http.Response, error) {
		wireCalls.Add(1)
		copyReq := req.Clone(req.Context())
		copyReq.Header.Set("User-Agent", "stable-credential-client")
		return base.RoundTrip(copyReq)
	})}
	clients := gateway.ClientProvider(func(ctx context.Context, selected gateway.Target) (*http.Client, error) {
		clientSelections.Add(1)
		if ctx.Value(policyTraceKey{}) != "preserved" || selected.ChannelID != 17 || selected.CredentialID != 19 || selected.Secret != target.Secret {
			t.Error("request context or selected credential was lost")
		}
		return configured, nil
	})
	ctx := native.WithRequest(context.WithValue(context.Background(), policyTraceKey{}, "preserved"), original, target, clients)
	fallback := &http.Client{Transport: policyTransportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unconfigured fallback transport used")
		return nil, errors.New("fallback transport forbidden")
	})}
	p := New(fallback)
	r, err := p.Submit(ctx, target, native.Submit{Model: original.Model, Body: original.Body})
	if err != nil || r.ID != operation || r.Status != "queued" {
		t.Fatalf("submit result %+v error %v", r, err)
	}
	task := native.Task{UpstreamID: r.ID, Model: model, Data: r.Data}
	r, err = p.Poll(ctx, target, task)
	if err != nil || r.Status != "completed" || r.Units != 4 {
		t.Fatalf("poll result %+v error %v", r, err)
	}
	task.Data = r.Data
	response, err := p.Content(ctx, target, task)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != 200 || string(data) != "stable-video" {
		t.Fatalf("content %q status %d errors %v %v", data, response.StatusCode, readErr, closeErr)
	}
	if tokenCalls.Load() != 1 || wireCalls.Load() != 4 || clientSelections.Load() != 4 {
		t.Errorf("OAuth, transport, client selection counts %d %d %d", tokenCalls.Load(), wireCalls.Load(), clientSelections.Load())
	}
	after, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("original billing model/principal/body/headers were mutated")
	}
}

func TestUnavailableCredentialClientDoesNotFallBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"failed", errors.New("credential client unavailable")},
		{"missing", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var selected, fallbackCalls atomic.Int32
			target := gateway.Target{ChannelID: 17, CredentialID: 19, Provider: "vertex", BaseURL: "http://unreachable.invalid", Secret: `{"project_id":"fixture","access_token":"fixture-access"}`, UpstreamModel: "veo-3.1-generate-preview"}
			original := &gateway.Request{Model: "client-veo", Body: []byte(`{"prompt":"original"}`), Principal: gateway.Principal{UserID: 7, KeyID: 9, Group: "paid"}}
			clients := gateway.ClientProvider(func(_ context.Context, target gateway.Target) (*http.Client, error) {
				selected.Add(1)
				if target.CredentialID != 19 {
					t.Error("wrong credential selected")
				}
				return nil, tc.err
			})
			fallback := &http.Client{Transport: policyTransportFunc(func(*http.Request) (*http.Response, error) {
				fallbackCalls.Add(1)
				return nil, errors.New("unconfigured transport called")
			})}
			ctx := native.WithRequest(context.Background(), original, target, clients)
			_, err := New(fallback).Submit(ctx, target, native.Submit{Model: original.Model, Body: original.Body})
			if err == nil || selected.Load() != 1 || fallbackCalls.Load() != 0 {
				t.Fatalf("client selection %d fallback %d error %v", selected.Load(), fallbackCalls.Load(), err)
			}
		})
	}
}
