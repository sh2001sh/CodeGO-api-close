package gateway

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestOverrideOperationsAndNestedNegativeIndices(t *testing.T) {
	tests := []struct{ name, body, operations, path, want string }{
		{"nested negative", `{"a":[{"b":[1,2]},{"b":[3,4]}]}`, `[{"mode":"set","path":"a.-1.b.-1","value":9}]`, "a.1.b.1", "9"},
		{"copy and move", `{"a":{"value":1},"items":["first","last"]}`, `[{"mode":"copy","from":"a.value","to":"copied"},{"mode":"move","from":"items.-1","to":"moved"}]`, "moved", "last"},
		{"prepend array", `{"items":[2]}`, `[{"mode":"prepend","path":"items","value":[1]}]`, "items", "[1,2]"},
		{"append array", `{"items":[2]}`, `[{"mode":"append","path":"items","value":[3]}]`, "items", "[2,3]"},
		{"keep object", `{"x":{"a":1}}`, `[{"mode":"append","path":"x","value":{"a":2,"b":3},"keep_origin":true}]`, "x.a", "1"},
		{"replace object", `{"x":{"a":1}}`, `[{"mode":"prepend","path":"x","value":{"a":2}}]`, "x.a", "2"},
		{"keep scalar", `{"x":1}`, `[{"mode":"set","path":"x","value":2,"keep_origin":true}]`, "x", "1"},
		{"string transforms", `{"x":" PreHELLOPost "}`, `[{"mode":"trim_space","path":"x"},{"mode":"trim_prefix","path":"x","value":"Pre"},{"mode":"trim_suffix","path":"x","value":"Post"},{"mode":"to_lower","path":"x"},{"mode":"ensure_prefix","path":"x","value":"<"},{"mode":"ensure_suffix","path":"x","value":">"},{"mode":"to_upper","path":"x"}]`, "x", "<HELLO>"},
		{"replace regex", `{"x":"hello 123"}`, `[{"mode":"replace","path":"x","from":"hello","to":"bye"},{"mode":"regex_replace","path":"x","from":"[0-9]+","to":"N"}]`, "x", "bye N"},
		{"append prepend string", `{"x":"middle"}`, `[{"mode":"prepend","path":"x","value":"A"},{"mode":"append","path":"x","value":"Z"}]`, "x", "AmiddleZ"},
		{"wildcard delete", `{"items":[1,2,3,4,5,6,7,8,9,10,11]}`, `[{"mode":"delete","path":"items.*"}]`, "items", "[]"},
		{"wildcard object", `{"x":{"alpha":{"v":1},"beta":{"v":2}}}`, `[{"mode":"set","path":"x.*.v","value":5}]`, "x.beta.v", "5"},
		{"prune recursive", `{"items":[{"type":"bad"},{"type":"ok","children":[{"type":"bad"},{"type":"ok"}]}]}`, `[{"mode":"prune_objects","path":"items","value":"bad"}]`, "items.0.children.#", "1"},
		{"prune root", `{"items":[{"type":"bad"},{"type":"ok"}]}`, `[{"mode":"prune_objects","value":{"where":{"type":"bad"}}}]`, "items.#", "1"},
		{"same-path move", `{"x":1}`, `[{"mode":"move","from":"x","to":"x"}]`, "x", "1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := overrideRequest(t, test.body)
			target := overrideTarget(t, `{"operations":`+test.operations+`}`)
			if err := ApplyUpstreamRequest(out, &Request{}, target); err != nil {
				t.Fatal(err)
			}
			body := overrideBody(t, out)
			if got := gjson.GetBytes(body, test.path).String(); got != test.want {
				t.Fatalf("got %s want %s; body %s", got, test.want, body)
			}
		})
	}
}

func TestOverrideConditionModesContextAndMissingKeys(t *testing.T) {
	for _, mode := range []string{"full", "prefix", "suffix", "contains", "gt", "gte", "lt", "lte"} {
		t.Run(mode, func(t *testing.T) {
			value := `"abc"`
			expect := `"abc"`
			switch mode {
			case "prefix":
				expect = `"a"`
			case "suffix":
				expect = `"c"`
			case "contains":
				expect = `"b"`
			case "gt", "gte":
				value = "3"
				expect = "2"
			case "lt", "lte":
				value = "1"
				expect = "2"
			}
			out := overrideRequest(t, `{"x":`+value+`}`)
			target := overrideTarget(t, `{"operations":[{"mode":"set","path":"passed","value":true,"conditions":[{"path":"x","mode":"`+mode+`","value":`+expect+`}]}]}`)
			if err := ApplyUpstreamRequest(out, &Request{}, target); err != nil {
				t.Fatal(err)
			}
			if !gjson.GetBytes(overrideBody(t, out), "passed").Bool() {
				t.Fatal("condition failed")
			}
		})
	}
	out := overrideRequest(t, `{"x":1}`)
	target := overrideTarget(t, `{"operations":[{"mode":"set","path":"passed","value":true,"logic":"AND","conditions":[{"path":"missing","mode":"full","value":1,"pass_missing_key":true},{"path":"x","mode":"full","value":2,"invert":true},{"path":"key_id","mode":"full","value":8}]}]}`)
	if err := ApplyUpstreamRequest(out, &Request{Principal: Principal{KeyID: 8}}, target); err != nil {
		t.Fatal(err)
	}
	if !gjson.GetBytes(overrideBody(t, out), "passed").Bool() {
		t.Fatal("missing/context/inverted AND condition failed")
	}
}

func TestOverrideConditionsCannotImpersonatePrincipalOrSecretHeaders(t *testing.T) {
	out := overrideRequest(t, `{"user_id":999,"request_headers":{"authorization":"client-secret"},"last_error":{"status_code":500}}`)
	target := overrideTarget(t, `{"operations":[{"mode":"set","path":"spoofed","value":true,"conditions":{"user_id":999}},{"mode":"set","path":"secret_spoofed","value":true,"conditions":{"request_headers.authorization":"client-secret"}},{"mode":"set","path":"retry_spoofed","value":true,"conditions":{"last_error.status_code":500}}]}`)
	if err := ApplyUpstreamRequest(out, &Request{Principal: Principal{UserID: 123}}, target); err != nil {
		t.Fatal(err)
	}
	body := overrideBody(t, out)
	for _, path := range []string{"spoofed", "secret_spoofed", "retry_spoofed"} {
		if gjson.GetBytes(body, path).Exists() {
			t.Fatalf("caller impersonated trusted context: %s", path)
		}
	}
}
