package jimeng

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestSigningMatchesIndependentPythonFixtureAndPreservesBody(t *testing.T) {
	body := []byte(`{"req_key":"jimeng_vgfm_t2v_l20","prompt":"hello"}`)
	req, err := http.NewRequest("POST", "https://visual.volcengineapi.com/?Version=2022-08-31&Action=CVSync2AsyncSubmitTask", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	if err := signRequest(req, body, "fixture-access", "fixture-secret", now); err != nil {
		t.Fatal(err)
	}
	want := "HMAC-SHA256 Credential=fixture-access/20260930/cn-north-1/cv/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=c0eded250db1f85224134760223405385b4663a4312e5865bbf998f0c2c9a032"
	if req.Header.Get("Authorization") != want {
		t.Fatalf("signature mismatch\n%s", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-Content-Sha256") != "247cfc0041368c158cace39fc5941c5729f7f8c08d639c7d7a75f88b2d3c4395" {
		t.Error("payload hash differs")
	}
	after, err := io.ReadAll(req.Body)
	if err != nil || !bytes.Equal(after, body) {
		t.Fatal("signer changed request body")
	}
}

func TestSignerCanonicalizesQueryOrderAndSpaces(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	var signature string
	for _, query := range []string{"B=hello+world&A=two&A=one", "A=one&B=hello%20world&A=two"} {
		req, err := http.NewRequest("POST", "https://visual.volcengineapi.com/?"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := signRequest(req, nil, "fixture-access", "fixture-secret", now); err != nil {
			t.Fatal(err)
		}
		if signature == "" {
			signature = req.Header.Get("Authorization")
		} else if signature != req.Header.Get("Authorization") {
			t.Error("equivalent query signed differently")
		}
	}
}
