//go:build pgintegration

package catalogcontrol

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type batchEncrypter struct {
	enc   catalog.Encrypter
	calls int
	fail  bool
}

func (e *batchEncrypter) Encrypt(raw []byte) ([]byte, error) {
	e.calls++
	if e.fail && e.calls == 2 {
		return nil, errors.New("injected encryption failure")
	}
	return e.enc.Encrypt(raw)
}

func TestLegacyBatchCreateIsAtomicAndSecretsRemainPrivate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	enc, err := catalog.NewAESGCM(bytes.Repeat([]byte{16}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES('default')`); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []bool{true, false} {
		s := New(pool, &batchEncrypter{enc: enc, fail: failure}, nil)
		mux := http.NewServeMux()
		s.Register(mux, func(h http.Handler) http.Handler { return h })
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/channel/", strings.NewReader(`{"mode":"batch","batch_add_set_key_prefix_2_name":true,"channel":{"name":"batch","type":1,"models":"gpt","key":"top-secret-one\ntop-secret-two"}}`)))
		want := 200
		if failure {
			want = 503
		}
		if w.Code != want {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_catalog.channels`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		expected := 2
		if failure {
			expected = 0
		}
		if count != expected {
			t.Fatalf("batch left %d channels, want %d", count, expected)
		}
		if !failure {
			var secrets int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_catalog.channel_credentials`).Scan(&secrets); err != nil || secrets != 2 {
				t.Fatalf("credentials=%d err=%v", secrets, err)
			}
			var leaked bool
			if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_catalog.channels WHERE name LIKE '%secret%')`).Scan(&leaked); err != nil || leaked {
				t.Fatal("batch names leak credentials")
			}
		}
		if strings.Contains(w.Body.String(), "top-secret") {
			t.Fatal("response leaked credentials")
		}
	}
}
