package native

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type identityTransport struct{ target gateway.Target }

func (i identityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.Header.Set("User-Agent", i.target.Fingerprint.UserAgent)
	return http.DefaultTransport.RoundTrip(out)
}

func TestNativePolicyUsesFrozenInputsAndSelectedCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if gjson.GetBytes(body, "temperature").Int() != 1 || gjson.GetBytes(body, "service_tier").Exists() || gjson.GetBytes(body, "store").Exists() {
			t.Errorf("channel settings/conditional body operations missing: %s", body)
		}
		if r.Header.Get("User-Agent") != "stable-credential-identity" || r.Header.Get("X-Route") != "frozen" || r.Header.Get("Authorization") != "Bearer configured-fixture" {
			t.Errorf("selected identity/header overrides missing: %v", r.Header)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"id":"upstream"}`))
	}))
	defer server.Close()
	original := &gateway.Request{Model: "alias", Body: []byte(`{"model":"alias","temperature":9}`), Principal: gateway.Principal{UserID: 7, KeyID: 8, Group: "default"}, PricingHeaders: map[string]string{"X-Route": "frozen"}}
	before, _ := json.Marshal(original)
	target := gateway.Target{ChannelID: 11, CredentialID: 12, Provider: "native", BaseURL: server.URL, Secret: "configured-fixture", UpstreamModel: "converted", Group: "paid",
		Fingerprint: gateway.CredentialFingerprint{UserAgent: "stable-credential-identity", TLSProfile: "chrome"}, Settings: map[string]any{"disable_store": true},
		HeaderOverride: map[string]string{"Authorization": "Bearer {api_key}", "X-Route": "{client_header:X-Route}"}, StatusCodeMapping: map[string]int{"429": 200}}
	if err := json.Unmarshal([]byte(`{"operations":[{"mode":"set","path":"temperature","value":1,"logic":"AND","conditions":{"original_model":"alias","model":"converted","user_id":7,"using_group":"paid"}}]}`), &target.ParamOverride); err != nil {
		t.Fatal(err)
	}
	selections := 0
	clients := func(_ context.Context, selected gateway.Target) (*http.Client, error) {
		selections++
		if selected.ChannelID != 11 || selected.CredentialID != 12 || selected.Fingerprint.TLSProfile != "chrome" {
			t.Fatal("credential identity changed")
		}
		return &http.Client{Transport: identityTransport{selected}}, nil
	}
	ctx := WithTarget(WithRequest(context.Background(), original, target, clients), target)
	req, _ := Request(ctx, http.MethodPost, server.URL, "old-fixture", []byte(`{"model":"converted","temperature":9,"service_tier":"priority","store":true}`))
	if _, err := JSON(nil, req); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(original)
	if string(before) != string(after) || selections != 1 {
		t.Fatalf("frozen request changed or credential not selected: %d", selections)
	}
}

func TestNativePolicyMapsRejectionAndBlocksInvalidConfigBeforeDispatch(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	target := gateway.Target{StatusCodeMapping: map[string]int{"200": 422}}
	req, _ := Request(WithTarget(context.Background(), target), http.MethodPost, server.URL, "", []byte(`{}`))
	_, err := JSON(nil, req)
	var rejected *Rejected
	if !errors.As(err, &rejected) || rejected.Status != 422 {
		t.Fatalf("mapped status not classified: %v", err)
	}
	for _, bad := range []gateway.Target{{StatusCodeMapping: map[string]int{"200": 700}}, {ParamOverride: map[string]any{"operations": "invalid"}}} {
		req, _ = Request(WithTarget(context.Background(), bad), http.MethodPost, server.URL, "", []byte(`{}`))
		_, err = JSON(nil, req)
		var invalid *InvalidRequest
		if !errors.As(err, &invalid) {
			t.Fatalf("invalid channel policy dispatched: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid requests reached upstream: %d", calls)
	}
}

func TestSelectedCredentialClientFailureNeverFallsBack(t *testing.T) {
	for _, failure := range []bool{false, true} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
		clients := func(context.Context, gateway.Target) (*http.Client, error) {
			if failure {
				return nil, errors.New("sensitive-fixture-secret")
			}
			return nil, nil
		}
		ctx := WithRequest(context.Background(), &gateway.Request{}, gateway.Target{}, clients)
		req, _ := Request(ctx, http.MethodPost, server.URL, "", []byte(`{}`))
		_, err := JSON(nil, req)
		server.Close()
		if err == nil || strings.Contains(err.Error(), "sensitive-fixture-secret") || calls != 0 {
			t.Fatalf("fallback or sensitive error: %v calls=%d", err, calls)
		}
	}
}

func TestPrepareIsIdempotentAndBodylessReadsRetainHeaderPolicy(t *testing.T) {
	target := gateway.Target{Settings: map[string]any{"system_prompt": "policy", "system_prompt_override": true}, HeaderOverride: map[string]string{"X-Test": "applied"}, ParamOverride: map[string]any{"value": 1}}
	req, _ := Request(WithTarget(context.Background(), target), http.MethodPost, "http://fixture.invalid", "", []byte(`{"messages":[{"role":"system","content":"original"}]}`))
	if err := Prepare(req); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(req); err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(req.Body)
	if gjson.GetBytes(body, "messages.0.content").String() != "policy\noriginal" {
		t.Fatalf("operations applied multiple times: %s", body)
	}
	req, _ = Request(WithTarget(context.Background(), target), http.MethodGet, "http://fixture.invalid", "", nil)
	if err := Prepare(req); err != nil || req.Header.Get("X-Test") != "applied" {
		t.Fatalf("bodyless read lost headers: %v", err)
	}
}
