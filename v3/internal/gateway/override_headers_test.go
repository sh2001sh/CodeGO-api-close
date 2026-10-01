package gateway

import (
	"reflect"
	"testing"

	"github.com/tidwall/gjson"
)

func TestOverrideHeadersTrustBoundary(t *testing.T) {
	req := &Request{PricingHeaders: map[string]string{"Authorization": "client-secret", "X-Api-Key": "client-secret", "Cookie": "client-cookie", "ChatGPT-Account-ID": "client-account", "OpenAI-Organization": "client-org", "X-Amz-Security-Token": "client-secret", "X-Test": "safe"}, ClientHeaders: map[string]string{"Anthropic-Beta": "beta"}}
	out := overrideRequest(t, `{}`)
	target := Target{HeaderOverride: map[string]string{"*": "", "Authorization": "Bearer {api_key}"}, Secret: "channel-secret"}
	if err := ApplyUpstreamRequest(out, req, target); err != nil {
		t.Fatal(err)
	}
	if out.Header.Get("Authorization") != "Bearer channel-secret" || out.Header.Get("X-Test") != "safe" || out.Header.Get("Anthropic-Beta") != "beta" {
		t.Fatal("allowed configured/client headers missing")
	}
	for _, name := range []string{"X-Api-Key", "Cookie", "ChatGPT-Account-ID", "OpenAI-Organization", "X-Amz-Security-Token"} {
		if out.Header.Get(name) != "" {
			t.Fatalf("unsafe passthrough %s", name)
		}
	}
	for _, config := range []map[string]string{{"X-Test": "{client_header:Authorization}"}, {"X-Test": "{client_header:}"}, {"X-Test": "{client_header:X-Test}suffix"}, {"Bad Header": "value"}, {"X-Test": "value\r\nInjected: secret"}, {"re:[": ""}} {
		if err := ApplyUpstreamRequest(overrideRequest(t, `{}`), req, Target{HeaderOverride: config}); err == nil {
			t.Fatalf("invalid header config accepted: %v", config)
		}
	}
	out = overrideRequest(t, `{}`)
	target.HeaderOverride = map[string]string{"regex:^x-test$": "", "X-Optional": "{client_header:X-Missing}"}
	if err := ApplyUpstreamRequest(out, req, target); err != nil {
		t.Fatal(err)
	}
	if out.Header.Get("X-Test") != "safe" || out.Header.Get("Anthropic-Beta") != "" || out.Header.Get("X-Optional") != "" {
		t.Fatal("regex or missing optional placeholder differs")
	}
}

func TestOverrideHeaderOperationsAndSync(t *testing.T) {
	req := &Request{PricingHeaders: map[string]string{"X-Source": "original", "Anthropic-Beta": "old,keep", "X-Remove": "request"}}
	original := map[string]string{"X-Source": "original", "Anthropic-Beta": "old,keep", "X-Remove": "request"}
	target := overrideTarget(t, `{"operations":[{"mode":"pass_headers","value":{"names":["X-Source","Anthropic-Beta"]}},{"mode":"set_header","path":"Anthropic-Beta","value":{"old":"new","$append":["extra","new"]}},{"mode":"copy_header","from":"X-Source","to":"X-Copy"},{"mode":"move_header","from":"X-Copy","to":"X-Moved"},{"mode":"delete_header","path":"X-Remove"},{"mode":"sync_fields","from":"header:X-Moved","to":"json:metadata.trace"},{"mode":"sync_fields","from":"json:label","to":"header:X-Label"},{"mode":"set","path":"conditional","value":true,"conditions":{"header_override.x-moved":"original"}}]}`)
	out := overrideRequest(t, `{"label":"body-label"}`)
	out.Header.Set("X-Remove", "provider")
	out.Header.Set("X-Copy", "provider")
	if err := ApplyUpstreamRequest(out, req, target); err != nil {
		t.Fatal(err)
	}
	if out.Header.Get("X-Source") != "original" || out.Header.Get("X-Moved") != "original" || out.Header.Get("X-Copy") != "" || out.Header.Get("X-Remove") != "" || out.Header.Get("Anthropic-Beta") != "new,keep,extra" || out.Header.Get("X-Label") != "body-label" {
		t.Fatalf("bad header result %v", out.Header)
	}
	body := overrideBody(t, out)
	if gjson.GetBytes(body, "metadata.trace").String() != "original" || !gjson.GetBytes(body, "conditional").Bool() {
		t.Fatalf("bad body sync %s", body)
	}
	if !reflect.DeepEqual(req.PricingHeaders, original) {
		t.Fatal("client headers mutated")
	}
}

func TestOverrideHeadersApplyToBinaryButParamsFail(t *testing.T) {
	out := overrideRequest(t, "binary")
	out.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	if err := ApplyUpstreamRequest(out, &Request{}, Target{HeaderOverride: map[string]string{"X-Test": "present"}}); err != nil {
		t.Fatal(err)
	}
	if out.Header.Get("X-Test") != "present" || string(overrideBody(t, out)) != "binary" {
		t.Fatal("binary header override mutated content")
	}
	out = overrideRequest(t, "binary")
	out.Header.Set("Content-Type", "application/octet-stream")
	if err := ApplyUpstreamRequest(out, &Request{}, Target{ParamOverride: map[string]any{"x": 1}}); err == nil {
		t.Fatal("binary param override silently dropped")
	}
	for from, mapped := range map[int]int{200: 503, 400: 200, 503: 429} {
		if got := MapUpstreamStatus(from, Target{StatusCodeMapping: map[string]int{httpStatusKey(from): mapped}}); got != mapped {
			t.Fatal("explicit status remap ignored")
		}
	}
}

func httpStatusKey(status int) string { return overrideIndex(status) }
