package channelmarket

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoutePoolGroupOptionsRequiresSession(t *testing.T) {
	s := New(nil, nil, nil, Config{}, nil)
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (Actor, error) { return Actor{}, errors.New("missing session") })
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/marketplace/route-pools/group-options", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous candidates returned %d instead of 401: %s", w.Code, w.Body.String())
	}
}
