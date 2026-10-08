package marketplace

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminPoolsRequiresAdministrator(t *testing.T) {
	for _, tc := range []struct {
		name   string
		id     int64
		admin  bool
		err    error
		status int
	}{
		{"anonymous", 0, false, errors.New("unauthenticated"), http.StatusUnauthorized},
		{"authenticated user", 1, false, nil, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := (&Service{}).Handler(func(*http.Request) (int64, bool, error) { return tc.id, tc.admin, tc.err })
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/api/blind-box/admin/pools", nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d", w.Code, tc.status)
			}
		})
	}
}
