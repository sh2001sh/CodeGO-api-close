package channelmarket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShopMutationAuthorizationAndCSRF(t *testing.T) {
	s := New(nil, nil, nil, Config{}, nil)
	mux := http.NewServeMux()
	s.Register(mux, func(r *http.Request) (Actor, error) {
		if r.Header.Get("X-Test-Actor") == "owner" {
			return Actor{UserID: 1}, nil
		}
		return Actor{}, errors.New("missing session")
	})
	for _, test := range []struct {
		path, actor, origin string
		status              int
	}{
		{"/api/marketplace/shop/mine", "", "", 401},
		{"/api/marketplace/shop/mine", "owner", "https://foreign.example", 403},
		{"/api/marketplace/admin/shops/10001/review", "owner", "", 403},
	} {
		method := http.MethodPatch
		if strings.Contains(test.path, "review") {
			method = http.MethodPost
		}
		r := httptest.NewRequest(method, test.path, strings.NewReader(`{"name":"Harbour AI","description":"Text models."}`))
		r.Header.Set("X-Test-Actor", test.actor)
		r.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s returned %d instead of %d: %s", test.path, w.Code, test.status, w.Body.String())
		}
	}
	if _, err := s.ListShops(context.Background(), Actor{}, false); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing database silently returned shops: %v", err)
	}
}

func TestShopIdentityIsCanonical(t *testing.T) {
	for _, id := range []string{"10001", "1", "9223372036854775807"} {
		if !validShopID(id) {
			t.Fatalf("valid shop ID rejected: %s", id)
		}
	}
	for _, id := range []string{"0", "-1", "01", "+1", " 1", "1.0", "1e4", "9223372036854775808", "owner-1"} {
		if validShopID(id) {
			t.Fatalf("noncanonical shop ID accepted: %s", id)
		}
	}
}

func TestShopTextRequiresSafeNamesAndDescriptions(t *testing.T) {
	for _, name := range []string{"海港模型研究", "Harbour AI", "人工知能研究", "ذكاء اصطناعي"} {
		if _, err := normalizeGroupName(name); err != nil {
			t.Fatalf("valid shop label rejected: %s: %v", name, err)
		}
	}
	for _, name := range []string{"官方 CodeGo", "加微１２３４５６７８", "example.com", "Model\u202eShop", strings.Repeat("店", 41)} {
		if _, err := normalizeGroupName(name); err == nil {
			t.Fatalf("unsafe shop label accepted: %s", name)
		}
	}
	for _, description := range []string{"关注文本与多模态模型调用。", "Text and multimodal models."} {
		if _, err := normalizeGroupRemark(description); err != nil {
			t.Fatalf("valid description rejected: %v", err)
		}
	}
	for _, description := range []string{"请加微信 123456789", "https://example.com", strings.Repeat("店", 201)} {
		if _, err := normalizeGroupRemark(description); err == nil {
			t.Fatalf("unsafe description accepted: %s", description)
		}
	}
}
