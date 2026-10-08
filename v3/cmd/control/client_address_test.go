package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func TestControlTrustedClientAddressesKeepIndependentAuthenticationLimits(t *testing.T) {
	for _, trustedPeer := range []bool{false, true} {
		t.Run(fmt.Sprint(trustedPeer), func(t *testing.T) {
			id, err := identity.NewControl(nil, identity.ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			h := clientAddressHandler(id.Handler(), []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
			for i := range 22 {
				r := httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader("{"))
				r.RemoteAddr = "192.0.2.9:1000"
				if trustedPeer {
					r.RemoteAddr = "10.1.2.3:1000"
				}
				r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i%2+1))
				original := r.RemoteAddr
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				want := http.StatusBadRequest
				if !trustedPeer && i >= 10 || trustedPeer && i >= 20 {
					want = http.StatusTooManyRequests
				}
				if w.Code != want || r.RemoteAddr != original {
					t.Fatalf("attempt %d status=%d want=%d original peer mutated=%v", i, w.Code, want, r.RemoteAddr != original)
				}
			}
		})
	}
}
