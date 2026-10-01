package commerce

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestWalletPasswordAndFeeBoundaries(t *testing.T) {
	for _, password := range []string{"letters-only", "12345678", "short1", " password1", strings.Repeat("界", 24) + "1"} {
		if validWalletPassword(password) {
			t.Fatalf("invalid password accepted: length %d", len(password))
		}
	}
	for _, password := range []string{"payment123", "支付密码abc123", strings.Repeat("界", 23) + "ab1"} {
		if !validWalletPassword(password) {
			t.Fatalf("valid password rejected: length %d", len(password))
		}
	}
	for _, tc := range []struct{ amount, fee credits.Micro }{{10000, 100}, {10001, 101}, {math.MaxInt64, 92233720368547759}} {
		if fee := walletFee(tc.amount); fee != tc.fee {
			t.Fatalf("fee(%d)=%d want %d", tc.amount, fee, tc.fee)
		}
	}
	if maskWalletName("张小明") != "张*明" || maskWalletEmail("sender@example.test") != "sr***@example.test" {
		t.Fatal("public fields are not masked")
	}
}

func TestWalletHandlersRejectForgedIdentityAndCredentials(t *testing.T) {
	mux := http.NewServeMux()
	h := &handler{authenticate: func(*http.Request) (Actor, error) { return Actor{}, errors.New("bad session") }}
	h.registerWallet(mux)
	for _, path := range []string{"/api/wallet/transfers", "/api/wallet/transfers/payment-password/email-code"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"user_id":1}`))
		r.Header.Set("New-Api-User", "1")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("forged identity accepted: %d", w.Code)
		}
	}
	mux = http.NewServeMux()
	h.authenticate = func(*http.Request) (Actor, error) { return Actor{UserID: 1}, nil }
	h.registerWallet(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/wallet/transfers", strings.NewReader(`{"recipient_external_id":"ABC234","amount_micro":10000,"sender_user_id":2}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("client-supplied sender accepted: %d", w.Code)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/wallet/transfers/payment-password/email-code", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"success":false`) {
		t.Fatalf("fake email success: %s", w.Body.String())
	}
}

func TestWalletPasswordLockedHTTPIncludesLegacyCode(t *testing.T) {
	w := httptest.NewRecorder()
	respondWallet(w, nil, ErrWalletPasswordLocked)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), `"code":"PAYMENT_PASSWORD_LOCKED"`) {
		t.Fatalf("lock response: %d %s", w.Code, w.Body.String())
	}
}
