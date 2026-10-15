package commerce

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

func TestBlindBoxCashCheckoutAndCodeIssuanceAreDisabled(t *testing.T) {
	s := New(nil, nil, nil, Config{})
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (Actor, error) { return Actor{UserID: 1, Role: "admin"}, nil })
	for _, path := range []string{"/api/blind-box/amount", "/api/blind-box/pay", "/api/redemption/"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{"name":"disabled","redeem_type":"blind_box","blind_box_quantity":1}`)))
		if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), marketplace.ErrBlindBoxesDisabled.Error()) {
			t.Fatalf("new issuance allowed at %s: status=%d body=%s", path, w.Code, w.Body)
		}
	}
	// Existing codes cannot be exchanged for new inventory. Already-used
	// codes still replay their saved result before this grant function runs.
	result := RedemptionResult{RedeemType: "blind_box", BlindBoxQuantity: 1}
	if err := s.applyRedemptionTx(context.Background(), nil, 1, "disabled", &result, Plan{}); !errors.Is(err, marketplace.ErrBlindBoxesDisabled) {
		t.Fatalf("existing blind-box code can still grant: %v", err)
	}
	if result.BlindBoxOrderID != 0 {
		t.Fatal("disabled redemption created an inventory order")
	}
}

func TestBlindBoxHistoricalPaymentAndCancellationRoutesRemainRegistered(t *testing.T) {
	s := New(nil, nil, nil, Config{})
	mux := http.NewServeMux()
	s.Register(mux, nil)
	for _, route := range []string{
		"GET /api/blind-box/orders/historical",
		"POST /api/blind-box/orders/historical/cancel",
		"GET /api/blind-box/epay/notify",
		"POST /api/blind-box/xunhu/notify",
		"GET /api/redemption/",
		"DELETE /api/redemption/1",
	} {
		method, path, _ := strings.Cut(route, " ")
		if _, pattern := mux.Handler(httptest.NewRequest(method, path, nil)); pattern == "" {
			t.Fatalf("historical settlement route removed: %s", route)
		}
	}
	var result RedemptionResult
	if err := decodeRedemptionResult([]byte(`{"redeem_type":"blind_box","blind_box_quantity":2,"blind_box_order_id":12}`), &result); err != nil || result.BlindBoxOrderID != 12 || result.BlindBoxQuantity != 2 {
		t.Fatalf("historical redemption result lost: %+v err=%v", result, err)
	}
}
