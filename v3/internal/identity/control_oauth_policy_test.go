package identity

import (
	"errors"
	"testing"
)

func TestOAuthPoliciesRetainNestedRulesAndExactNumericClaims(t *testing.T) {
	info := []byte(`{"user":{"level":9007199254740993,"roles":["member","trusted"],"region":"china-east"},"active":true}`)
	for _, policy := range []string{
		`{"conditions":[{"field":"user.level","op":"gt","value":9007199254740992}]}`,
		`{"logic":"or","conditions":[{"field":"missing","op":"exists"}],"groups":[{"conditions":[{"field":"user.roles","op":"contains","value":"trusted"},{"field":"user.region","op":"in","value":["china-east","other"]},{"field":"active","op":"eq","value":true}]}]}`,
		`{"conditions":[{"field":"user.region","op":"not_contains","value":"west"},{"field":"missing","op":"not_exists"}]}`,
	} {
		if err := checkOAuthPolicy(info, policy); err != nil {
			t.Fatalf("allowed policy=%s error=%v", policy, err)
		}
	}
	denied := `{"conditions":[{"field":"user.level","op":"eq","value":9007199254740992}]}`
	err := checkOAuthPolicy(info, denied)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("integer boundary granted access: %v", err)
	}
	var failure *oauthPolicyDenial
	if !errors.As(err, &failure) {
		t.Fatal("denial lost context")
	}
	message := renderOAuthDenial(`{{provider}}: {{field}} needs {{required}}, got {{current.user.level}}`, "Custom", info, failure)
	if message != "Custom: user.level needs 9007199254740992, got 9007199254740993" {
		t.Fatalf("denial template=%s", message)
	}
	for _, policy := range []string{`{}`, `{"logic":"xor","conditions":[{"field":"active","op":"eq","value":true}]}`, `{"conditions":[{"field":"active","op":"in","value":true}]}`, `{"conditions":[{"field":"active","op":"unsupported","value":true}]}`} {
		if checkOAuthPolicy(info, policy) == nil {
			t.Fatalf("invalid policy silently granted access: %s", policy)
		}
	}
	for _, claim := range []string{`{}`, `{"trust_level":null}`, `{"trust_level":"untrusted"}`, `{"trust_level":true}`, `{"trust_level":0}`} {
		if err := checkOAuthPolicy([]byte(claim), `{"conditions":[{"field":"trust_level","op":"gte","value":1}]}`); !errors.Is(err, ErrForbidden) {
			t.Fatalf("invalid trust claim admitted: %s error=%v", claim, err)
		}
	}
}
