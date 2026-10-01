package bedrock

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignMatchesAWSSDKReference(t *testing.T) {
	body := []byte(`{"messages":[]}`)
	req, err := http.NewRequest("POST", "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude%3A0/invoke?hello=two%20words&z=last", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	cred := credential{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "example-secret", SessionToken: "example-session", Region: "us-east-1"}
	if err := signRequest(req, body, cred, time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	// Fixture produced independently by AWS SDK v2 signer with the same request.
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20240102/us-east-1/bedrock/aws4_request, SignedHeaders=accept;content-length;content-type;host;x-amz-date;x-amz-security-token, Signature=381bac4dcf91313b920600e71a8f7dfad2341e191535bf7b5ffae4bffe7b9984"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("signature mismatch: %s", got)
	}
	if req.Header.Get("X-Amz-Date") != "20240102T030405Z" || req.Header.Get("X-Amz-Security-Token") != "example-session" {
		t.Fatal("signing headers absent")
	}
}

func TestSigningCanonicalQueryAndMutation(t *testing.T) {
	body := []byte(`{}`)
	build := func(raw string) *http.Request {
		req, err := http.NewRequest("POST", raw, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	a := build("https://example.com/model/m/invoke?a=z&a=a&b=two+words")
	b := build("https://example.com/model/m/invoke?b=two%20words&a=a&a=z")
	cred := credential{AccessKeyID: "test", SecretAccessKey: "secret", Region: "us-east-1"}
	for _, req := range []*http.Request{a, b} {
		if err := signRequest(req, body, cred, time.Unix(0, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if a.Header.Get("Authorization") != b.Header.Get("Authorization") {
		t.Fatal("query order changed signature")
	}
	b.Header.Set("Content-Type", "text/plain")
	if err := signRequest(b, body, cred, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if a.Header.Get("Authorization") == b.Header.Get("Authorization") {
		t.Fatal("signature did not bind request headers")
	}
}

func TestCredentialFormats(t *testing.T) {
	for _, test := range []struct {
		secret, base string
		signed       bool
		region       string
	}{
		{"test-bearer|us-east-1", "", false, "us-east-1"},
		{"test-access|test-secret|eu-west-1", "", true, "eu-west-1"},
		{"test-access|test-secret|ap-northeast-1|test-session", "", true, "ap-northeast-1"},
		{`{"access_key_id":"test","secret_access_key":"secret","session_token":"session"}`, "https://bedrock-runtime.us-east-1.amazonaws.com", true, "us-east-1"},
		{`{"api_key":"test"}`, "https://bedrock-runtime.cn-north-1.amazonaws.com.cn", false, "cn-north-1"},
		{"test-bearer", "https://bedrock-runtime.eu-central-1.amazonaws.com", false, "eu-central-1"},
	} {
		got, err := credentials(test.secret, test.base)
		if err != nil {
			t.Fatal(err)
		}
		if got.Region != test.region || (got.AccessKeyID != "") != test.signed {
			t.Fatalf("wrong credentials: %+v", got)
		}
	}
}

func TestCredentialsRejectInvalidWithoutExposingSecrets(t *testing.T) {
	for _, value := range []string{"", "private-secret", "private-secret|", "private-secret|invalid.region", "private-secret|secret|us-east-1|", "private-secret\r\n|us-east-1", `{"api_key":"private-secret","access_key_id":"a","region":"us-east-1"}`, `{"access_key_id":"private-secret","region":"us-east-1"}`, `{"api_key":"private-secret"`} {
		_, err := credentials(value, "")
		if err == nil {
			t.Fatalf("accepted invalid credentials %q", value)
		}
		if strings.Contains(err.Error(), "private-secret") {
			t.Fatal("credential leaked in error")
		}
	}
}
