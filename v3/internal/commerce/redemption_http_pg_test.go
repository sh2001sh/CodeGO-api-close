//go:build pgintegration

package commerce_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestTypedRedemptionHTTPReturnsProtectedTypedBenefit(t *testing.T) {
	s, _, _ := newService(t)
	actor := commerce.Actor{UserID: 1, Role: "admin"}
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) {
		if actor.UserID == 0 {
			return actor, errors.New("no session")
		}
		return actor, nil
	})
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(body)))
		return r
	}
	w := call("POST", "/api/redemption/", `{"name":"money","credits":5000,"redeem_type":"credits"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("issue=%d %s", w.Code, w.Body.String())
	}
	var issued struct {
		Data commerce.RedemptionCode `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	actor.Role = "user"
	for _, method := range []string{"GET", "POST"} {
		w = call(method, "/api/redemption/", `{"name":"bad","credits":1}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("admin access=%d", w.Code)
		}
	}
	for _, path := range []string{"/api/commerce/redemptions/redeem", "/api/user/topup"} {
		body, _ := json.Marshal(map[string]string{"key": issued.Data.Key})
		w = call("POST", path, string(body))
		var result struct {
			Data commerce.RedemptionResult `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || result.Data.RedeemType != "credits" || result.Data.Credits != 5000 {
			t.Fatalf("response=%d %s", w.Code, w.Body.String())
		}
	}
	actor.UserID = 0
	if w = call("POST", "/api/commerce/redemptions/redeem", `{"key":"someone-else"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous=%d", w.Code)
	}
	list, err := s.ListRedemptions(context.Background(), 0, 10)
	if err != nil || len(list) != 1 || list[0].Key != "" || list[0].Credits != 5000 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}
