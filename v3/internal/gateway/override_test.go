package gateway

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func overrideRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	out, err := http.NewRequest(http.MethodPost, "http://provider.test/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Authorization", "Bearer provider-secret")
	return out
}

func overrideTarget(t *testing.T, value string) Target {
	t.Helper()
	target := Target{Secret: "configured-secret", UpstreamModel: "mapped-model", Group: "paid"}
	if err := unmarshalOverrideString(value, &target.ParamOverride); err != nil {
		t.Fatal(err)
	}
	return target
}

func overrideBody(t *testing.T, out *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestOverrideNativeRequestPreservesOriginalBillingInputs(t *testing.T) {
	client := []byte(`{"model":"alias","temperature":0.8,"service_tier":"priority"}`)
	req := &Request{Body: bytes.Clone(client), Model: "alias", Principal: Principal{UserID: 123, KeyID: 456, Group: "default"}, Reserve: &struct{}{}, PricingHeaders: map[string]string{"X-Route": "paid"}}
	reservation := req.Reserve
	target := overrideTarget(t, `{"literal.key":"kept","operations":[{"mode":"set","path":"temperature","value":0,"logic":"AND","conditions":{"model":"mapped-model","user_id":123,"request_headers.x-route":"paid","using_group":"paid"}},{"mode":"set_header","path":"X-Policy","value":"configured"}]}`)
	target.HeaderOverride = map[string]string{"Authorization": "Bearer {api_key}", "X-Route": "{client_header:X-Route}"}
	out := overrideRequest(t, `{"model":"mapped-model","temperature":0.8,"service_tier":"priority"}`)
	if err := ApplyUpstreamRequest(out, req, target); err != nil {
		t.Fatal(err)
	}
	body := overrideBody(t, out)
	if gjson.GetBytes(body, "temperature").Float() != 0 || gjson.GetBytes(body, "model").String() != "mapped-model" || gjson.GetBytes(body, `literal\.key`).String() != "kept" || gjson.GetBytes(body, "service_tier").Exists() {
		t.Fatalf("incorrect native body: %s", body)
	}
	if !bytes.Equal(req.Body, client) || req.Reserve != reservation || !reflect.DeepEqual(req.PricingHeaders, map[string]string{"X-Route": "paid"}) {
		t.Fatal("original billing input mutated")
	}
	if out.Header.Get("Authorization") != "Bearer configured-secret" || out.Header.Get("X-Route") != "paid" || out.Header.Get("X-Policy") != "configured" {
		t.Fatal("headers were not applied")
	}
	copyBody, err := out.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := copyBody.Close(); err != nil {
			t.Error(err)
		}
	}()
	copyBytes, _ := io.ReadAll(copyBody)
	if !bytes.Equal(copyBytes, body) || out.ContentLength != int64(len(body)) {
		t.Fatal("body metadata is stale")
	}
}

func TestOverrideReachesActualHTTPServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-DashScope-Plugin") != "plugin-json" || r.Header.Get("X-Test") != "one" || gjson.GetBytes(body, "native.value").Int() != 42 {
			t.Error("override missing at upstream")
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	out := overrideRequest(t, `{"model":"native"}`)
	upstreamRequest, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	out.URL = upstreamRequest.URL
	target := overrideTarget(t, `{"operations":[{"mode":"set","path":"native.value","value":42}]}`)
	target.Provider = "ali"
	target.Settings = map[string]any{"api_version": "plugin-json"}
	target.HeaderOverride = map[string]string{"X-Test": "one"}
	target.StatusCodeMapping = map[string]int{"429": 400}
	if err := ApplyUpstreamRequest(out, &Request{Model: "alias"}, target); err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	if MapUpstreamStatus(resp.StatusCode, target) != 400 || MapUpstreamStatus(200, target) != 200 {
		t.Fatal("status mapping changed unrelated HTTP success")
	}
}

func TestOverrideReturnErrorAndInvalidConfiguration(t *testing.T) {
	target := overrideTarget(t, `{"operations":[{"mode":"return_error","value":{"message":"policy blocked","status_code":422,"code":"policy","type":"policy_error","skip_retry":false}}]}`)
	err := ApplyUpstreamRequest(overrideRequest(t, `{"model":"x"}`), &Request{Model: "x"}, target)
	var upstream *UpstreamError
	var blocked *ParamOverrideReturnError
	if !errors.As(err, &upstream) || !errors.As(err, &blocked) || upstream.Status != 422 || upstream.Code != "policy" || upstream.Type != "policy_error" || upstream.Message != "policy blocked" || blocked.SkipRetry {
		t.Fatalf("intentional error lost: %v", err)
	}
	for _, invalid := range []string{`{"operations":{}}`, `{"operations":[null]}`, `{"operations":[{"mode":"set","path":"x","conditions":null}]}`, `{"operations":[{"mode":"set","path":"x","conditions":{}}]}`, `{"operations":[{"mode":"prune_objects","value":{"type":"bad","recursive":"false"}}]}`, `{"operations":[{"mode":"unknown","conditions":{"missing":1}}]}`, `{"operations":[{"mode":"set","path":"x","conditions":[{"path":"missing","mode":"unknown"}]}]}`, `{"operations":[{"mode":"set","path":"items.-2","value":1}]}`, `{"operations":[{"mode":"return_error","value":{"message":"block","status":42}}]}`} {
		if err := ApplyUpstreamRequest(overrideRequest(t, `{"model":"x","items":[1]}`), &Request{Model: "x"}, overrideTarget(t, invalid)); err == nil {
			t.Fatalf("invalid config accepted: %s", invalid)
		}
	}
	target = Target{StatusCodeMapping: map[string]int{"429": 700}}
	if err := ApplyUpstreamRequest(overrideRequest(t, `{"model":"x"}`), &Request{}, target); err == nil {
		t.Fatal("invalid status accepted")
	}
}
