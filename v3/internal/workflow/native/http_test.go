package native

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestRejectHTTPFailureAndBlockCredentialRedirect(t *testing.T) {
	var leak bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leak = true
		if _, err := w.Write([]byte(`{}`)); err != nil {
			t.Error(err)
		}
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	req, _ := Request(context.Background(), http.MethodPost, source.URL, "private-key", []byte(`{}`))
	_, err := JSON(nil, req)
	var rejected *Rejected
	if !errors.As(err, &rejected) || rejected.Status != 307 || leak {
		t.Fatalf("redirect status=%v leak=%v", err, leak)
	}
}

func TestChannelProxyUsesRequestContextAndErrorsRemainSafe(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "provider.invalid" || r.Header.Get("Authorization") != "Bearer sample-secret" {
			t.Errorf("proxy request %s %+v", r.URL, r.Header)
		}
		if _, err := w.Write([]byte(`{"status":"queued"}`)); err != nil {
			t.Error(err)
		}
	}))
	defer proxy.Close()
	ctx := WithTarget(context.Background(), gateway.Target{ProxyURL: proxy.URL})
	req, _ := Request(ctx, http.MethodPost, "http://provider.invalid/tasks", "sample-secret", []byte(`{}`))
	body, err := JSON(nil, req)
	if err != nil || string(body) != `{"status":"queued"}` {
		t.Fatalf("proxy response=%s err=%v", body, err)
	}
	req, _ = Request(WithTarget(context.Background(), gateway.Target{ProxyURL: "bad://sample-secret"}), http.MethodPost, "http://provider.invalid/tasks", "sample-secret", nil)
	_, err = JSON(nil, req)
	if err == nil || strings.Contains(err.Error(), "sample-secret") {
		t.Fatalf("unsafe proxy error: %v", err)
	}
}
