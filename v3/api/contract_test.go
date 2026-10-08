package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type contractOperation struct {
	OperationID  string                     `json:"operationId"`
	Responses    map[string]json.RawMessage `json:"responses"`
	Security     []map[string][]string      `json:"security"`
	RequiredRole string                     `json:"x-required-role"`
}

func contractPaths(t *testing.T) map[string]map[string]contractOperation {
	t.Helper()
	var document struct {
		Paths map[string]map[string]contractOperation `json:"paths"`
	}
	if err := json.Unmarshal(specification, &document); err != nil {
		t.Fatal(err)
	}
	return document.Paths
}

// Compare actual domain route declarations with the published contract. This
// catches new endpoints omitted from generation and validates exact-path {$}
// aliases instead of silently treating them as subtree endpoints.
func TestContractCoversDeclaredDomainRoutes(t *testing.T) {
	paths := contractPaths(t)
	seen := map[string]bool{}
	check := func(pattern, source string) {
		t.Helper()
		method, path, ok := strings.Cut(pattern, " ")
		if !ok || !strings.HasPrefix(path, "/") {
			return
		}
		if seen[pattern] {
			return
		}
		seen[pattern] = true
		path = strings.ReplaceAll(path, "{$}", "")
		if op, exists := paths[path][strings.ToLower(method)]; !exists || op.OperationID == "" {
			t.Errorf("%s declares %s without a generated operation", source, pattern)
		}
	}
	for _, domain := range []string{"identity", "catalogcontrol", "commerce", "marketplace", "audit", "community", "channelmarket", "control", "security", "desktop", "incentives", "adminops", "notifications"} {
		err := filepath.WalkDir(filepath.Join("..", "internal", domain), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				var literal *ast.BasicLit
				switch n := node.(type) {
				case *ast.BasicLit:
					// Guard.Register keeps complete route declarations in a string
					// array. Other domains concatenate route prefixes, so scanning
					// every literal there would mistake a prefix for a full route.
					if domain == "security" {
						literal = n
					}
				case *ast.CallExpr:
					selector, ok := n.Fun.(*ast.SelectorExpr)
					if ok && (selector.Sel.Name == "Handle" || selector.Sel.Name == "HandleFunc") && len(n.Args) > 0 {
						literal, _ = n.Args[0].(*ast.BasicLit)
					}
					if name, ok := n.Fun.(*ast.Ident); ok && (name.Name == "register" || name.Name == "bind") && len(n.Args) > 0 {
						literal, _ = n.Args[0].(*ast.BasicLit)
					}
				case *ast.KeyValueExpr:
					literal, _ = n.Key.(*ast.BasicLit)
				}
				if literal != nil && literal.Kind == token.STRING {
					if value, err := strconv.Unquote(literal.Value); err == nil {
						check(value, path)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestContractCoversDynamicAliases(t *testing.T) {
	paths := contractPaths(t)
	check := func(method, path string) {
		t.Helper()
		if paths[path][method].OperationID == "" {
			t.Errorf("missing %s %s", method, path)
		}
	}
	for _, provider := range []string{"stripe", "epay", "creem", "xunhu", "nowpayments", "waffo", "waffo-pancake"} {
		path := "/api/user/" + provider + "/pay"
		if provider == "epay" {
			path = "/api/user/pay"
		}
		check("post", path)
		check("post", "/api/subscription/"+provider+"/pay")
	}
	for _, provider := range []string{"epay", "xunhu"} {
		for _, prefix := range []string{"/api/user/", "/api/subscription/", "/api/blind-box/"} {
			for _, method := range []string{"get", "post"} {
				check(method, prefix+provider+"/notify")
				check(method, prefix+provider+"/return")
			}
		}
	}
	for _, path := range []string{"/api/log/", "/api/log/self", "/api/log/search", "/api/log/self/search", "/api/log/token", "/api/audit/usage", "/api/blind-box/self", "/api/blind-box/inventory/overview"} {
		check("get", path)
	}
}

func TestContractKeepsCredentialClassesDistinct(t *testing.T) {
	paths := contractPaths(t)
	for _, tc := range []struct{ path, method, scheme string }{
		{"/api/log/token", "get", "gatewayApiKey"},
		{"/api/community/v1/members/{sub}", "get", "communitySecret"},
		{"/api/oidc/userinfo", "get", "oidcAccessToken"},
	} {
		security := paths[tc.path][tc.method].Security
		if len(security) != 1 || len(security[0]) != 1 {
			t.Fatalf("%s auth classes mixed: %#v", tc.path, security)
		}
		if _, ok := security[0][tc.scheme]; !ok {
			t.Errorf("%s needs %s: %#v", tc.path, tc.scheme, security)
		}
	}
	if _, present := paths["/api/oidc/authorize"]["get"].Responses["200"]; present {
		t.Fatal("OIDC authorize must document a redirect, not a wrapped JSON success")
	}
	if strings.Contains(string(specification), "#/components/schemas/Empty") {
		t.Fatal("ordinary DTOs still fall back to unknown Empty schema")
	}
}

func TestHistoricalAuditAcceptsSeparatelyScopedReadOnlyKeys(t *testing.T) {
	paths := contractPaths(t)
	for _, path := range []string{"/api/audit/events", "/api/audit/events/export", "/api/audit/requests", "/api/audit/requests/{request}/attempts"} {
		security := paths[path]["get"].Security
		if len(security) != 3 {
			t.Fatalf("%s must accept session or read-only key transports: %#v", path, security)
		}
		for i, name := range []string{"sessionCookie", "sessionBearer", "gatewayApiKey"} {
			if len(security[i]) != 1 {
				t.Fatalf("%s combines credential requirements: %#v", path, security)
			}
			if _, ok := security[i][name]; !ok {
				t.Fatalf("%s missing %s alternative", path, name)
			}
		}
	}
	for _, path := range []string{"/api/audit/usage", "/api/audit/usage/stat", "/api/billing/history", "/api/billing/balance"} {
		for _, transport := range paths[path]["get"].Security {
			if _, hasKey := transport["gatewayApiKey"]; hasKey {
				t.Fatalf("%s cannot grant wallet/session access to an API key", path)
			}
		}
	}
}

func TestBrowserCommunityContractUsesSessionIdentity(t *testing.T) {
	paths := contractPaths(t)
	for _, path := range []string{"/api/community/sellers", "/api/community/channels/{id}/rating"} {
		method := "get"
		if strings.HasSuffix(path, "/rating") {
			method = "post"
		}
		security := paths[path][method].Security
		if len(security) != 2 {
			t.Fatalf("%s must support the two session transports: %#v", path, security)
		}
		for i, name := range []string{"sessionCookie", "sessionBearer"} {
			if len(security[i]) != 1 {
				t.Fatalf("%s mixes session and service authentication: %#v", path, security)
			}
			if _, ok := security[i][name]; !ok {
				t.Fatalf("%s needs %s: %#v", path, name, security)
			}
		}
	}
	var document struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specification, &document); err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Type    string `json:"type"`
			Minimum int    `json:"minimum"`
			Maximum int    `json:"maximum"`
		} `json:"properties"`
		Required             []string `json:"required"`
		AdditionalProperties bool     `json:"additionalProperties"`
	}
	if err := json.Unmarshal(document.Components.Schemas["CommunitySessionRatingInput"], &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Properties) != 1 || schema.AdditionalProperties || len(schema.Required) != 1 || schema.Required[0] != "stars" {
		t.Fatal("browser rating must accept only required stars, never a caller-supplied subject")
	}
	stars := schema.Properties["stars"]
	if stars.Type != "integer" || stars.Minimum != 1 || stars.Maximum != 5 {
		t.Fatalf("invalid browser rating bounds: %#v", stars)
	}
}

func TestBlindBoxAliasesAndScalarMoneyContracts(t *testing.T) {
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type     string `json:"type"`
					Nullable bool   `json:"nullable"`
				} `json:"properties"`
				Required []string          `json:"required"`
				AnyOf    []json.RawMessage `json:"anyOf"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specification, &document); err != nil {
		t.Fatal(err)
	}
	grant := document.Components.Schemas["BoxGrantInput"]
	for _, field := range []string{"count", "quantity", "request_id", "idempotency_key", "pool_id", "reason"} {
		if _, exists := grant.Properties[field]; !exists {
			t.Fatalf("blind-box admin compatibility field missing: %s", field)
		}
	}
	if len(grant.Required) != 0 || len(grant.AnyOf) != 2 {
		t.Fatal("blind-box count and quantity must be alternatives, not individually mandatory")
	}
	gift := document.Components.Schemas["GiftInput"]
	if gift.Properties["recipient_external_id"].Type != "string" || len(gift.AnyOf) != 2 {
		t.Fatal("blind-box gifts must document numeric and external recipient alternatives")
	}
	usage := document.Components.Schemas["ChannelMarketUserUsage"].Properties["amount_micro"]
	if usage.Type != "integer" || usage.Nullable {
		t.Fatal("required usage money cannot inherit nullability from optional input IDs")
	}
}
