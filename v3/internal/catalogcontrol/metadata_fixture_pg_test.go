//go:build pgintegration

package catalogcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type metadataFixture struct {
	pool   *pgxpool.Pool
	server *Server
	cipher *catalog.AESGCM
	mux    *http.ServeMux
}

func newMetadataFixture(t *testing.T) metadataFixture {
	t.Helper()
	pool := testPool(t)
	enc, err := catalog.NewAESGCM(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	server := New(pool, enc, nil)
	mux := http.NewServeMux()
	server.Register(mux, func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Test-Admin") != "yes" {
				fail(w, 403, "forbidden", "Administrator authorization is required")
				return
			}
			handler.ServeHTTP(w, r)
		})
	})
	return metadataFixture{pool: pool, server: server, cipher: enc, mux: mux}
}

func (f metadataFixture) call(t *testing.T, method, path, body string, want int) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Test-Admin", "yes")
	f.mux.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: %d, want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	return w
}

func metadataResultID(t *testing.T, w *httptest.ResponseRecorder) int64 {
	t.Helper()
	var result struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data.ID <= 0 {
		t.Fatalf("invalid metadata result: %s: %v", w.Body.String(), err)
	}
	return result.Data.ID
}

func metadataExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
