package legacy

import (
	"testing"
)

func TestBuildPricesPreservesV2CoefficientsAndLockedCompletion(t *testing.T) {
	prices, err := buildPrices(map[string]string{
		"ModelRatio":                   `{"claude-sonnet-4":1.5,"custom":0.1234567}`,
		"CompletionRatio":              `{"claude-sonnet-4":99,"custom":3}`,
		"CacheRatio":                   `{"claude-sonnet-4":0.1}`,
		"CreateCacheRatio":             `{"claude-sonnet-4":1.25}`,
		"ModelPrice":                   `{"image":0.04}`,
		"billing_setting.billing_mode": `{"tiered-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"tiered-model":"len <= 200 ? p * 2 + c * 8 : p * 4 + c * 12"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	p := prices["claude-sonnet-4"]
	if p.InputPerMTok != 3000000 || p.OutputPerMTok != 15000000 || p.CacheReadPerMTok != 300000 || p.CacheWritePerMTok != 3750000 {
		t.Fatalf("locked model price=%+v", p)
	}
	if p := prices["custom"]; p.InputPerMTok != 246913 || p.OutputPerMTok != 740740 {
		t.Fatalf("exact decimal price=%+v", p)
	}
	if p := prices["image"]; p.Mode != "per_request" || p.PerRequest != 40000 {
		t.Fatalf("request price=%+v", p)
	}
	if p := prices["tiered-model"]; p.Mode != "expression" {
		t.Fatalf("expression price=%+v", p)
	}
}

func TestBuildPricesRejectsInvalidConfiguration(t *testing.T) {
	for _, options := range []map[string]string{
		{"ModelRatio": `{"model":-1}`},
		{"ModelPrice": `{"model":10000000000000000000000}`},
		{"billing_setting.billing_mode": `{"model":"tiered_expr"}`},
	} {
		if _, err := buildPrices(options); err == nil {
			t.Fatalf("accepted invalid price configuration")
		}
	}
}

func TestSplitSecretsPreservesCompoundAndOAuthKeys(t *testing.T) {
	for _, value := range []string{"access,secret,region", `{"access_token":"opaque","refresh_token":"other"}`} {
		parts := splitSecrets(value)
		if len(parts) != 1 || parts[0] != value {
			t.Fatalf("compound credential split: %v", parts)
		}
	}
	if parts := splitSecrets("first\nsecond\n"); len(parts) != 2 {
		t.Fatalf("multikey=%v", parts)
	}
}

func TestCredentialPropertiesPreservesOAuthAndDisabledKey(t *testing.T) {
	kind, status, expiry, err := credentialProperties(`{"access_token":"opaque","refresh_token":"rotating","expired":"2027-01-01T00:00:00Z"}`,
		[]byte(`{"multi_key_status_list":{"0":2}}`), 0)
	if err != nil || kind != "oauth" || status != "disabled" || expiry == nil || expiry.Year() != 2027 {
		t.Fatalf("kind=%s status=%s expiry=%v err=%v", kind, status, expiry, err)
	}
	if _, _, _, err = credentialProperties(`{"refresh_token":"rotating"}`, nil, 0); err == nil {
		t.Fatal("missing expiry accepted")
	}
}
