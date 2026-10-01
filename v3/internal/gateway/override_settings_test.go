package gateway

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestOverrideActualV2SettingsAndAdministratorRestore(t *testing.T) {
	for _, test := range []struct {
		name          string
		settings      map[string]any
		expectPresent bool
	}{
		{"default filters", nil, false},
		{"explicit allows", map[string]any{"allow_service_tier": true, "allow_inference_geo": true, "allow_speed": true, "allow_safety_identifier": true, "allow_include_obfuscation": true}, true},
		{"passthrough", map[string]any{"pass_through_body_enabled": true, "disable_store": true}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := overrideRequest(t, `{"model":"x","service_tier":"priority","inference_geo":"us","speed":"fast","safety_identifier":"id","stream_options":{"include_obfuscation":false},"store":true}`)
			if err := ApplyUpstreamRequest(out, &Request{}, Target{Settings: test.settings}); err != nil {
				t.Fatal(err)
			}
			body := overrideBody(t, out)
			for _, path := range []string{"service_tier", "inference_geo", "speed", "safety_identifier", "stream_options.include_obfuscation"} {
				if gjson.GetBytes(body, path).Exists() != test.expectPresent {
					t.Fatalf("wrong %s filter: %s", path, body)
				}
			}
			if !gjson.GetBytes(body, "store").Bool() {
				t.Fatal("default/passthrough store should stay enabled")
			}
		})
	}
	out := overrideRequest(t, `{"model":"x","store":true,"service_tier":"auto"}`)
	target := Target{Settings: map[string]any{"disable_store": true}, ParamOverride: map[string]any{"service_tier": "priority"}}
	if err := ApplyUpstreamRequest(out, &Request{}, target); err != nil {
		t.Fatal(err)
	}
	body := overrideBody(t, out)
	if gjson.GetBytes(body, "store").Exists() || gjson.GetBytes(body, "service_tier").String() != "priority" {
		t.Fatalf("settings override order differs from v2 %s", body)
	}
	if err := ApplyUpstreamRequest(overrideRequest(t, `{}`), &Request{}, Target{Settings: map[string]any{"allow_speed": "true"}}); err == nil {
		t.Fatal("invalid boolean setting silently ignored")
	}
}

func TestOverrideSystemPromptNativeShapes(t *testing.T) {
	for _, test := range []struct {
		name, body, path, want string
		anthropic, replace     bool
	}{
		{"chat missing", `{"messages":[{"role":"user","content":"hello"}]}`, "messages.0.content", "configured", false, false},
		{"chat preserves", `{"messages":[{"role":"system","content":"original"}]}`, "messages.0.content", "original", false, false},
		{"chat prefixes", `{"messages":[{"role":"system","content":"original"}]}`, "messages.0.content", "configured\noriginal", false, true},
		{"chat structured", `{"messages":[{"role":"system","content":[{"type":"text","text":"original"}]}]}`, "messages.0.content.0.text", "configured", false, true},
		{"anthropic missing", `{"model":"x","max_tokens":4,"messages":[{"role":"user","content":"hello"}]}`, "system", "configured", true, false},
		{"anthropic structured", `{"system":[{"type":"text","text":"original","cache_control":{"type":"ephemeral"}}],"messages":[]}`, "system.1.cache_control.type", "ephemeral", true, true},
		{"responses", `{"input":"hello","instructions":"original"}`, "instructions", "configured\noriginal", false, true},
		{"gemini", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, "systemInstruction.parts.0.text", "configured", false, false},
		{"gemini preserves", `{"contents":[],"systemInstruction":{"parts":[{"text":"original"}]}}`, "systemInstruction.parts.0.text", "original", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := overrideRequest(t, test.body)
			if test.anthropic {
				out.Header.Set("Anthropic-Version", "2023-06-01")
			}
			target := Target{Settings: map[string]any{"system_prompt": "configured", "system_prompt_override": test.replace}}
			if err := ApplyUpstreamRequest(out, &Request{}, target); err != nil {
				t.Fatal(err)
			}
			if body := overrideBody(t, out); gjson.GetBytes(body, test.path).String() != test.want {
				t.Fatalf("system prompt wrong %s", body)
			}
		})
	}
}

func TestOverrideRawModeKeepsPreparedFilesAndStillRunsOperations(t *testing.T) {
	req := &Request{Model: "alias", Body: []byte(`{"model":"alias","input":[{"file_id":"local-file"}]}`), Path: "/v1/responses"}
	out := overrideRequest(t, `{"model":"alias","input":[{"file_data":"prepared-file-bytes"}],"instructions":"client prompt","service_tier":"priority"}`)
	target := overrideTarget(t, `{"operations":[{"mode":"set","path":"raw_condition","value":true,"conditions":{"request_path":"/v1/responses"}},{"mode":"set_header","path":"X-Raw","value":"configured"}]}`)
	target.Settings = map[string]any{"pass_through_body_enabled": true, "system_prompt": "must not replace client", "system_prompt_override": true, "disable_store": true}
	if err := ApplyUpstreamRequest(out, req, target); err != nil {
		t.Fatal(err)
	}
	body := overrideBody(t, out)
	if gjson.GetBytes(body, "input.0.file_data").String() != "prepared-file-bytes" || gjson.GetBytes(body, "input.0.file_id").Exists() {
		t.Fatal("prepared files were replaced by original request")
	}
	if gjson.GetBytes(body, "instructions").String() != "client prompt" || gjson.GetBytes(body, "service_tier").String() != "priority" {
		t.Fatal("raw mode applied conversion settings")
	}
	if !gjson.GetBytes(body, "raw_condition").Bool() || out.Header.Get("X-Raw") != "configured" {
		t.Fatal("raw mode dropped operations or original client path")
	}
	if gjson.GetBytes(req.Body, "input.0.file_id").String() != "local-file" {
		t.Fatal("original client body mutated")
	}
}

func TestOverrideResponseBooleanValidationAlsoPrecedesBinaryDispatch(t *testing.T) {
	for _, field := range []string{"force_format", "thinking_to_content", "pass_through_body_enabled"} {
		for _, contentType := range []string{"application/json", "application/octet-stream", "multipart/form-data; boundary=test"} {
			out := overrideRequest(t, `{}`)
			out.Header.Set("Content-Type", contentType)
			if err := ApplyUpstreamRequest(out, &Request{}, Target{Settings: map[string]any{field: "true"}}); err == nil {
				t.Fatalf("invalid %s accepted for %s", field, contentType)
			}
		}
	}
}
