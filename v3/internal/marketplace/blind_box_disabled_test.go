package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlindBoxMutationsAreDisabledBeforeInventoryOrMoneyAccess(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, Config{})
	h := s.Handler(func(*http.Request) (int64, bool, error) { return 1, true, nil })
	for _, route := range []string{
		"POST /api/blind-box/inventory/open",
		"POST /api/blind-box/inventory/purchase",
		"POST /api/blind-box/inventory/gift",
		"POST /api/blind-box/props/1/use",
		"POST /api/blind-box/props/1/convert",
		"POST /api/blind-box/props/1/gift",
		"PUT /api/blind-box/admin/pools",
		"POST /api/blind-box/admin/users/1/grants",
		"POST /api/blind-box/simulation/draw",
		"POST /api/blind-box/batches/1/simulate",
		"POST /api/blind-box/batches/1/draw",
		"PUT /api/blind-box/admin/batches",
		"POST /api/blind-box/admin/batches/1/publish",
	} {
		t.Run(route, func(t *testing.T) {
			method, path, _ := strings.Cut(route, " ")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || w.Code != http.StatusGone || body.Success || body.Message != ErrBlindBoxesDisabled.Error() {
				t.Fatalf("route remains active: status=%d body=%s", w.Code, w.Body)
			}
		})
	}
}

func TestBlindBoxHistoryAndRevocationRoutesRemainRegistered(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, Config{})
	mux := s.Handler(nil).(*http.ServeMux)
	for _, route := range []string{
		"GET /api/blind-box/history",
		"GET /api/blind-box/self",
		"GET /api/blind-box/admin/users/1/overview",
		"POST /api/blind-box/admin/users/1/revoke",
		"DELETE /api/blind-box/admin/users/1/grants",
		"POST /api/blind-box/props/1/pause",
		"POST /api/blind-box/admin/batches/1/pause",
		"POST /api/group-buy/create",
	} {
		method, path, _ := strings.Cut(route, " ")
		r := httptest.NewRequest(method, path, nil)
		if _, pattern := mux.Handler(r); pattern == "" || disabledBlindBoxMutation(r) {
			t.Fatalf("historical or unrelated operation closed: %s", route)
		}
	}
	if n, err := s.AccrueBatchEntitlements(context.Background(), 1000); err != nil || n != 0 {
		t.Fatalf("retired promotion issued boxes: count=%d err=%v", n, err)
	}
}
