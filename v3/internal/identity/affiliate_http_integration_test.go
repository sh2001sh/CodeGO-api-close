//go:build pgintegration

package identity

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAffiliateLegacyHTTPAmountsAndTypedRetry(t *testing.T) {
	c, _, _, _, _ := affiliateFixture(t)
	u, err := c.User(ctx, 7)
	if err != nil || u.AffiliateMicroCredits != 3000000 {
		t.Fatalf("funds DTO=%+v %v", u, err)
	}
	s, err := c.NewSession(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	call := func(body, version, operation string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/user/aff_transfer", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+s.AccessToken)
		if version != "" {
			r.Header.Set("X-CodeGo-API-Version", version)
		}
		if operation != "" {
			r.Header.Set("Idempotency-Key", operation)
		}
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{`{"quota":499999}`, `{"quota":-1}`, `{"quota":9223372036854775807}`, `{"quota":500000,"user_id":8}`, `{"quota":500000,"amount_micro_credits":1000000}`} {
		if w := call(body, "", ""); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid old money admitted: %s status=%d body=%s", body, w.Code, w.Body.String())
		}
	}
	for i := 0; i < 2; i++ {
		if w := call(`{"quota":500000}`, "", "legacy-retry"); w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"data"`) {
			t.Fatalf("legacy response/retry changed: status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if w := call(`{"amount_micro_credits":1000000}`, "3", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("typed caller lacks idempotency: %d", w.Code)
	}
	if w := call(`{"amount_micro_credits":2000000,"operation_id":"typed-retry"}`, "3", "mismatch"); w.Code != http.StatusBadRequest {
		t.Fatalf("conflicting retry IDs accepted: %d", w.Code)
	}
	for i := 0; i < 2; i++ {
		w := call(`{"amount_micro_credits":2000000,"operation_id":"typed-retry"}`, "3", "")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"wallet_micro_credits":3000010`) || !strings.Contains(w.Body.String(), `"affiliate_micro_credits":0`) {
			t.Fatalf("native exact funds/retry failed: status=%d body=%s", w.Code, w.Body.String())
		}
	}
}
