package catalogcontrol

import (
	"reflect"
	"testing"
)

func TestLegacySecretArraysAndStructuredCredentials(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want []string
	}{
		{`["key-one",{"type":"service_account","private_key":"fixture-only"}]`, []string{"key-one", `{"type":"service_account","private_key":"fixture-only"}`}},
		{`{"access_token":"fixture-only","account_id":"one"}`, []string{`{"access_token":"fixture-only","account_id":"one"}`}},
		{`"key-one"`, []string{"key-one"}},
	} {
		got, err := legacySecrets(c.raw)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("structured credential parse failed: %v", err)
		}
	}
	for _, raw := range []string{`[null]`, `[123]`, `[`, `[true]`} {
		if _, err := legacySecrets(raw); err == nil {
			t.Fatal("invalid credential array accepted")
		}
	}
}
