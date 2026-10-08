package channelmarket

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisclosureNormalizationAndLimits(t *testing.T) {
	days := 30
	context, output := int64(128000), int64(4096)
	input := ChannelMarketDisclosureInput{SourceKind: "reseller", Regions: []string{"hk", "US", "HK"}, Retention: "limited", RetentionDays: &days, Training: "no", PolicyURL: "https://provider.example/privacy", Models: []ChannelModelDisclosure{{Model: "sample-model", Streaming: "supported", ContextTokens: &context, MaxOutputTokens: &output}}}
	got, err := normalizeDisclosure(input)
	if err != nil || len(got.Regions) != 2 || got.Regions[0] != "HK" || got.Models[0].Tools != "unknown" {
		t.Fatalf("normalize: %+v %v", got, err)
	}
	empty, err := normalizeDisclosure(ChannelMarketDisclosureInput{})
	if err != nil || empty.SourceKind != "unknown" || empty.Retention != "unknown" || empty.Training != "unknown" || empty.Models == nil || empty.Regions == nil {
		t.Fatalf("empty invented a claim: %+v %v", empty, err)
	}
	negative, excess := -1, 3651
	zero, huge := int64(0), int64(1_000_000_001)
	for _, bad := range []ChannelMarketDisclosureInput{
		{SourceKind: "verified"}, {Training: "never"}, {Regions: []string{"ZZ"}}, {Regions: []string{"USA"}},
		{Retention: "none", RetentionDays: &days}, {Retention: "limited", RetentionDays: &negative}, {Retention: "limited", RetentionDays: &excess},
		{Models: []ChannelModelDisclosure{{Model: "sample-model", Tools: "verified"}}},
		{Models: []ChannelModelDisclosure{{Model: "sample-model"}, {Model: "sample-model"}}},
		{Models: []ChannelModelDisclosure{{Model: "sample-model", ContextTokens: &zero}}},
		{Models: []ChannelModelDisclosure{{Model: "sample-model", ContextTokens: &huge}}},
		{Models: []ChannelModelDisclosure{{Model: "sample-model", ContextTokens: &output, MaxOutputTokens: &context}}},
		{Models: []ChannelModelDisclosure{{Model: "sample\u202emodel"}}},
	} {
		if _, err = normalizeDisclosure(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid declaration: %+v %v", bad, err)
		}
	}
}

func TestDisclosurePolicyURLsRejectAdvertisingAndContacts(t *testing.T) {
	for _, good := range []string{"https://provider.example/privacy", "https://provider.example/policies/data", "https://provider.example/legal/retention"} {
		if !validDisclosurePolicyURL(good) {
			t.Fatalf("valid policy rejected: %s", good)
		}
	}
	for _, bad := range []string{"http://provider.example/privacy", "https://provider.example/", "https://provider.example/shop/privacy", "https://t.me/privacy", "https://chat.discord.com/privacy", "https://provider.example/privacy?ref=123", "https://provider.example/privacy#contact", "https://user:pass@provider.example/privacy", "https://127.0.0.1/privacy", "https://localhost/privacy", "https://provider.local/privacy", "https://provider.example:8443/privacy", "https://provider.example/pri vacy"} {
		if validDisclosurePolicyURL(bad) {
			t.Fatalf("advertising/contact URL accepted: %s", bad)
		}
	}
}

func TestDisclosureCannotAcceptProvenanceFromClient(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"source_kind":"direct","provenance":"platform_verified"}`))
	w := httptest.NewRecorder()
	var input ChannelMarketDisclosureInput
	if decode(w, r, &input) || w.Code != 400 {
		t.Fatalf("client-controlled provenance accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestInsightsHTTPRejectsInvalidWindowAndRequiresSessionToWrite(t *testing.T) {
	s := New(nil, nil, nil, Config{}, nil)
	mux := http.NewServeMux()
	s.RegisterInsightsHTTP(mux, nil)
	for _, url := range []string{"/api/marketplace/groups/1/insights?model=sample&window_hours=1", "/api/marketplace/groups/1/insights?model=sample&window_hours=no", "/api/marketplace/groups/1/insights"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != 400 {
			t.Fatalf("bad query accepted: %s %d", url, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/marketplace/channels/1/disclosure", strings.NewReader(`{}`)))
	if w.Code != 403 {
		t.Fatalf("anonymous write accepted: %d", w.Code)
	}
	// Go's mux must coexist with the already-registered parent wildcard.
	mux.HandleFunc("GET /api/marketplace/groups/{slug}", func(http.ResponseWriter, *http.Request) {})
}

func TestInsightsUnknownMetricsStayAbsent(t *testing.T) {
	body, err := json.Marshal(ChannelMarketInsights{FailureCounts: []ModelFailureCount{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"success_rate", "wilson_success_rate", "ttft_p50_ms", "ttft_p95_ms", "avg_tps", "disclosure"} {
		if strings.Contains(string(body), `"`+key+`"`) {
			t.Fatalf("unknown observation serialized: %s", body)
		}
	}
}
