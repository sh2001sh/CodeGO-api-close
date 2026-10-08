//go:build pgintegration

package marketplace

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBlindBatchHTTPAuthorizationAndInvalidRequests(t *testing.T) {
	f := newFixture(t)
	h := f.s.Handler(func(r *http.Request) (int64, bool, error) {
		switch r.Header.Get("X-Test-User") {
		case "admin":
			return 1, true, nil
		case "user":
			return 2, false, nil
		default:
			return 0, false, errors.New("unauthenticated")
		}
	})
	call := func(method, path, user, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("X-Test-User", user)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	call("GET", "/api/blind-box/batches", "", "", 401)
	for _, path := range []string{"/api/blind-box/admin/batches", "/api/blind-box/admin/batches/1/stats"} {
		call("GET", path, "user", "", 403)
	}
	call("PUT", "/api/blind-box/admin/batches", "user", `{}`, 403)
	call("POST", "/api/blind-box/admin/batches/1/publish", "user", `{}`, 403)
	call("PUT", "/api/blind-box/admin/batches", "admin", `{"purpose":"credits","name":"bad","rewards":[{"kind":"multiplier"}]}`, 400)
	b := seedBatch(t, f, "credits", []BatchReward{{ID: "r", Title: "r", Kind: "credits", Amount: 10, Quantity: 1}})
	path := "/api/blind-box/batches/1/draw"
	if b.ID != 1 {
		t.Fatal("expected isolated first ID")
	}
	call("POST", path, "user", `{"request_id":"bad","count":101}`, 400)
	call("POST", path, "user", `{"request_id":"bad"} {}`, 400)
	w := call("POST", path, "user", `{"request_id":"http","count":1}`, 200)
	var out struct {
		Success bool
		Data    BatchDrawResult
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || !out.Success || len(out.Data.Records) != 1 || out.Data.Charged != 100 {
		t.Fatalf("draw contract %+v %v", out, err)
	}
	call("POST", path, "user", `{"request_id":"http","count":1}`, 200)
	call("POST", path, "user", `{"request_id":"http","count":2}`, 409)
	call("GET", "/api/blind-box/admin/batches/1/stats", "admin", "", 200)
}
