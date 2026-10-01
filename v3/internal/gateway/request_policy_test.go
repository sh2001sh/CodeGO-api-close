package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestRequestPolicyModelsAndMetadata(t *testing.T) {
	for _, tc := range []struct {
		models []string
		model  string
		denied bool
	}{
		{nil, "model", false}, {[]string{}, "model", true}, {[]string{"allowed"}, "allowed", false},
		{[]string{"allowed"}, "other", true}, {[]string{}, "", false},
	} {
		err := ValidateRequestPolicy(Principal{AllowedModels: tc.models}, tc.model, nil, nil)
		if (err != nil) != tc.denied {
			t.Fatalf("models %v model %s: %v", tc.models, tc.model, err)
		}
		if err != nil {
			var up *UpstreamError
			if !errors.As(err, &up) || up.Status != http.StatusForbidden || up.Code != "model_not_allowed" {
				t.Fatal(err)
			}
		}
	}
}

func TestRequestPolicyLeavesVirtualCardGroupEntitlementsToPlanner(t *testing.T) {
	for _, group := range []string{"auto", "zero-hour", "monthly-pass", "default"} {
		p := Principal{Group: group, AllowedGroups: []string{"default"}}
		if err := ValidateRequestPolicy(p, "model", nil, nil); err != nil {
			t.Fatalf("group %s was denied before planner entitlement validation: %v", group, err)
		}
	}
	if err := ValidateRequestPolicy(Principal{Group: "revoked", AllowedGroups: []string{"default"}}, "model", nil, nil); err == nil {
		t.Fatal("revoked ordinary group must be rejected")
	}
}

func TestRequestPolicyIPAndTrustedProxy(t *testing.T) {
	allow := []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
	trust := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	for _, tc := range []struct {
		remote, forwarded string
		trusted           []netip.Prefix
		denied            bool
	}{
		{"192.0.2.1:123", "", nil, false},
		{"[::ffff:192.0.2.1]:123", "", nil, false},
		{"198.51.100.1:123", "192.0.2.1", nil, true},
		{"10.0.0.1:123", "192.0.2.1", trust, false},
		{"10.0.0.1:123", "192.0.2.1, 198.51.100.2", trust, true},
		{"bad-address", "192.0.2.1", trust, true},
	} {
		r := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("X-Forwarded-For", tc.forwarded)
		err := ValidateRequestPolicy(Principal{AllowedCIDRs: allow}, "", r, tc.trusted)
		if (err != nil) != tc.denied {
			t.Fatalf("remote %s forwarded %s: %v", tc.remote, tc.forwarded, err)
		}
	}
	if err := ValidateRequestPolicy(Principal{AllowedCIDRs: []netip.Prefix{}}, "", httptest.NewRequest("GET", "/v1/files", nil), nil); err == nil {
		t.Fatal("empty IP list must deny")
	}
	if err := ValidateRequestPolicy(Principal{AllowedCIDRs: allow}, "", nil, nil); err == nil {
		t.Fatal("missing request bypassed IP restriction")
	}
}
