package gateway_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestVerifiedRequestIDReachesHTTPResponse(t *testing.T) {
	const verified = "market-test-00112233445566778899aabbccddeeff"
	calls := 0
	h := newHarness(t, gateway.Config{RequestID: func(r *http.Request) string {
		calls++
		if r.Header.Get("X-Test-Verified") == "true" {
			return verified
		}
		return ""
	}}, "complete/c4")
	for _, accepted := range []bool{true, false} {
		r, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
			h.gw.URL+"/v1/chat/completions", strings.NewReader(streamBody))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer sk-test")
		if accepted {
			r.Header.Set("X-Test-Verified", "true")
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("response: status=%d read=%v close=%v", resp.StatusCode, readErr, closeErr)
		}
		id := resp.Header.Get("X-Request-Id")
		if id == "" || (accepted && id != verified) || (!accepted && id == verified) {
			t.Fatalf("accepted=%v response id=%q", accepted, id)
		}
		_ = h.outcome()
	}
	if calls != 2 {
		t.Fatalf("request verifier called %d times", calls)
	}
}

func TestVerifiedRequestIDDoesNotBypassAuthentication(t *testing.T) {
	h := newHarness(t, gateway.Config{RequestID: func(*http.Request) string {
		return "market-test-00112233445566778899aabbccddeeff"
	}}, "complete/c4")
	r, err := http.NewRequest(http.MethodPost, h.gw.URL+"/v1/chat/completions", strings.NewReader(streamBody))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized || h.settler.reserved != 0 || len(h.planner.results()) != 0 {
		t.Fatalf("verified ID bypassed admission: status=%d reserved=%d", resp.StatusCode, h.settler.reserved)
	}
}
