package channelmarket

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowseHTTPRejectsUnsupportedShopSortBeforeStorage(t *testing.T) {
	s := New(nil, nil, nil, Config{}, nil)
	mux := http.NewServeMux()
	s.Register(mux, nil)
	for _, path := range []string{"/api/marketplace/groups?page=0", "/api/marketplace/shops?page=1&sort=price", "/api/marketplace/shops/42?page_size=101"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 400 {
			t.Fatalf("%s status=%d", path, response.Code)
		}
	}
}

func TestBrowseRejectsInvalidAndUnboundedInput(t *testing.T) {
	for _, query := range []string{"page=0", "page=-1", "page=1000001", "page=bad", "page_size=101", "tag=advertising", "scope=admin", "sort=drop", "price_basis=unknown"} {
		if _, err := parseBrowse(httptest.NewRequest("GET", "/?"+query, nil)); err == nil {
			t.Fatalf("accepted invalid browse query %s", query)
		}
	}
	o, err := parseBrowse(httptest.NewRequest("GET", "/?page=2&page_size=1&model=claude&search=%25&tag=anthropic", nil))
	if err != nil || o.Page != 2 || o.PageSize != 1 || o.Search != "%" || o.Sort != "recommended" {
		t.Fatalf("parse: %+v %v", o, err)
	}
}

func TestBrowsePriceUsesExactDecimalsAndKeepsUnknownLast(t *testing.T) {
	a := &PublicModelPrice{Mode: "per_token", InputPerMillion: "9007199254740992.000000000001"}
	b := &PublicModelPrice{Mode: "per_token", InputPerMillion: "9007199254740992.000000000002"}
	if compareBrowsePrice(a, b, "input") >= 0 {
		t.Fatal("prices rounded through float64")
	}
	if compareBrowsePrice(a, nil, "input") >= 0 || compareBrowsePrice(nil, a, "input") <= 0 {
		t.Fatal("missing quote presented as cheapest")
	}
	if compareBrowsePrice(&PublicModelPrice{Mode: "expression", InputPerMillion: "0"}, a, "input") <= 0 {
		t.Fatal("dynamic quote presented as free")
	}
	if _, value := browsePrice(a, "request"); value != nil {
		t.Fatal("token and per-request prices mixed")
	}
	left, right := browseCandidate{id: "a", publicID: "2", name: "same", price: a}, browseCandidate{id: "b", publicID: "3", name: "same", price: b}
	if !browseLess(left, right, BrowseOptions{Sort: "price", PriceBasis: "input"}) {
		t.Fatal("price order not applied globally")
	}
}
