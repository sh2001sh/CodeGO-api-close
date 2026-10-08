package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRestoredCredentialBoundaries(t *testing.T) {
	paths := contractPaths(t)
	var document struct {
		Security []map[string][]string `json:"security"`
	}
	if err := json.Unmarshal(specification, &document); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/desktop/account/summary", "/api/desktop/usage/logs",
		"/api/desktop/authorized-devices", "/api/desktop/tokens",
		"/api/desktop/config/templates", "/api/desktop/diagnostics/report",
	} {
		method := "get"
		if strings.HasSuffix(path, "/report") {
			method = "post"
		}
		security := paths[path][method].Security
		if len(security) != 1 || len(security[0]) != 1 {
			t.Fatalf("device route %s mixes credential classes: %#v", path, security)
		}
		if _, ok := security[0]["desktopDeviceToken"]; !ok {
			t.Fatalf("%s is missing scoped device authentication", path)
		}
	}
	for _, path := range []string{"/api/desktop/devices", "/api/desktop/auth/approve", "/api/user/2fa/setup"} {
		method := "get"
		if path != "/api/desktop/devices" {
			method = "post"
		}
		transports := paths[path][method].Security
		if transports == nil {
			transports = document.Security
		}
		if len(transports) != 2 {
			t.Fatalf("browser route %s requires session transports: %#v", path, transports)
		}
		for i, transport := range transports {
			if _, present := transport[[]string{"sessionCookie", "sessionBearer"}[i]]; !present || len(transport) != 1 {
				t.Fatalf("browser route %s grants a different credential class: %#v", path, transport)
			}
			if _, present := transport["desktopDeviceToken"]; present {
				t.Fatalf("device credentials grant browser access at %s", path)
			}
			if _, present := transport["gatewayApiKey"]; present {
				t.Fatalf("gateway keys grant browser access at %s", path)
			}
		}
	}
	for _, path := range []string{"/api/ratio_sync/channels", "/api/performance/stats"} {
		if paths[path]["get"].RequiredRole != "root" {
			t.Fatalf("%s does not require root", path)
		}
	}
	if paths["/api/deployments/"]["post"].RequiredRole != "admin" {
		t.Fatal("deployment creation requires an administrator session")
	}
}

func TestLoginContractIncludesPendingFactorWithoutSessionCredentials(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			Responses map[string]struct {
				Content map[string]struct {
					Schema struct {
						Properties map[string]struct {
							OneOf []struct {
								Ref string `json:"$ref"`
							} `json:"oneOf"`
						} `json:"properties"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specification, &doc); err != nil {
		t.Fatal(err)
	}
	choices := doc.Paths["/api/user/login"]["post"].Responses["200"].Content["application/json"].Schema.Properties["data"].OneOf
	if len(choices) != 2 || choices[0].Ref != "#/components/schemas/Session" || choices[1].Ref != "#/components/schemas/TwoFactorLoginChallenge" {
		t.Fatalf("login hides the pending-factor result: %#v", choices)
	}
	challenge := doc.Components.Schemas["TwoFactorLoginChallenge"].Properties
	if len(challenge) != 2 || challenge["require_2fa"] == nil || challenge["challenge_token"] == nil {
		t.Fatal("pending-factor DTO must not include access or refresh credentials")
	}
	register := doc.Components.Schemas["RegisterInput"].Properties
	for _, field := range []string{"verification_code", "aff_code"} {
		if register[field] == nil {
			t.Fatalf("registration omits restored field %s", field)
		}
	}
}

func TestRestoredRouterRejectsMalformedDeviceIDsBeforeDomainDispatch(t *testing.T) {
	calls := 0
	handler := DomainHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}), func(w http.ResponseWriter, _ *http.Request, _ error) { w.WriteHeader(http.StatusBadRequest) })
	for _, path := range []string{"/api/desktop/devices/not-an-integer", "/api/user/not-an-integer/2fa"} {
		r := httptest.NewRequest(http.MethodDelete, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("malformed %s dispatched to domain: status=%d calls=%d", path, w.Code, calls)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/desktop/devices/7", nil))
	if w.Code != http.StatusNoContent || calls != 1 {
		t.Fatalf("valid device route not dispatched: status=%d calls=%d", w.Code, calls)
	}
}

func TestDailyLuckyNumberIsAbsentFromPublishedContract(t *testing.T) {
	for path := range contractPaths(t) {
		if strings.HasPrefix(path, "/api/daily-lucky-number/") {
			t.Fatalf("removed lucky number endpoint remains published: %s", path)
		}
	}
}

func TestRetainedAffiliateOverviewDispatchesSameContract(t *testing.T) {
	paths := contractPaths(t)
	rewards, overview := paths["/api/user/aff/rewards"]["get"], paths["/api/user/aff/overview"]["get"]
	if overview.OperationID == "" || overview.OperationID == rewards.OperationID {
		t.Fatal("retained overview must have its own generated operation")
	}
	if string(overview.Responses["200"]) != string(rewards.Responses["200"]) {
		t.Fatal("retained overview changed the rewards response contract")
	}
	seen := []string{}
	handler := DomainHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}), func(w http.ResponseWriter, _ *http.Request, _ error) { w.WriteHeader(http.StatusBadRequest) })
	for _, path := range []string{"/api/user/aff/rewards", "/api/user/aff/overview"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNoContent || len(seen) == 0 || seen[len(seen)-1] != path {
			t.Fatalf("retained %s failed to dispatch: %d %#v", path, w.Code, seen)
		}
	}
}

func TestDesktopPriceKeepsOptionalRulesExact(t *testing.T) {
	for _, raw := range []string{
		`{"billing_expr":"p * 3","image_ratio":0.1234567890123456789,"audio_ratio":9007199254740993.000001,"audio_completion_ratio":0}`,
		`{}`,
	} {
		var value DesktopPrice
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var before, after map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &before); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &after); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"billing_expr", "image_ratio", "audio_ratio", "audio_completion_ratio"} {
			if string(before[field]) != string(after[field]) {
				t.Fatalf("optional pricing %s lost precision or was fabricated: before=%s after=%s", field, before[field], after[field])
			}
		}
	}
}
