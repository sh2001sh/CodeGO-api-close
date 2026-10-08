package identity

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyAcceptanceRejectsUnsupportedDocumentVersionAndLocale(t *testing.T) {
	for _, in := range []PolicyAcceptanceInput{
		{Document: "supplier", Version: "2026-10-04", Locale: "en"},
		{Document: "supplier", Version: CurrentPolicyVersion, Locale: "xx"},
		{Document: "advertising", Version: CurrentPolicyVersion, Locale: "en"},
		{},
	} {
		if !errors.Is(validatePolicyAcceptance(in), ErrInvalidInput) {
			t.Fatalf("invalid acceptance allowed: %+v", in)
		}
	}
	for _, locale := range []string{"zh-HK", "zh-CN", "en", "ja", "ru", "ko", "fr", "de", "ar"} {
		if err := validatePolicyAcceptance(PolicyAcceptanceInput{Document: "supplier", Version: CurrentPolicyVersion, Locale: locale}); err != nil {
			t.Fatalf("supported locale %s: %v", locale, err)
		}
	}
}

func TestPublicRegistrationRequiresExplicitCurrentPoliciesBeforeDatabase(t *testing.T) {
	c, _ := testControl(t)
	for _, body := range []string{
		`{"username":"alice","password":"long-password"}`,
		`{"username":"alice","password":"long-password","accepted_terms_version":"2026-10-04","accepted_privacy_version":"2026-10-04","agreement_locale":"en"}`,
		`{"username":"alice","password":"long-password","accepted_terms_version":"2026-10-07","accepted_privacy_version":"2026-10-07","agreement_locale":"xx"}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "https://codego.test/api/user/register", strings.NewReader(body))
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("registration acceptance status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestCurrentPoliciesArePublicAndCrossOriginAcceptanceIsRejected(t *testing.T) {
	c, _ := testControl(t)
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "https://codego.test/api/policies/current", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("current policies status=%d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data CurrentPolicies `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Data.Version != CurrentPolicyVersion || len(envelope.Data.Documents) != 3 {
		t.Fatalf("published policy versions=%+v err=%v", envelope, err)
	}
	r := httptest.NewRequest(http.MethodPost, "https://codego.test/api/user/policy-acceptance", strings.NewReader(`{"document":"supplier","version":"2026-10-07","locale":"en"}`))
	r.Header.Set("Origin", "https://attacker.test")
	w = httptest.NewRecorder()
	c.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin acceptance allowed: %d", w.Code)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		w = httptest.NewRecorder()
		c.Handler().ServeHTTP(w, httptest.NewRequest(method, "https://codego.test/api/user/policy-acceptance", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous policy acceptance method=%s status=%d", method, w.Code)
		}
	}
}

func TestInternalRegistrationDoesNotFabricateAgreement(t *testing.T) {
	if err := validateRegistrationPolicies(RegisterInput{}, false); err != nil {
		t.Fatal(err)
	}
	if err := validateRegistrationPolicies(RegisterInput{AcceptedTermsVersion: CurrentPolicyVersion}, false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("partial acceptance allowed: %v", err)
	}
}
