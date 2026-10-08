//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func TestSupplierAcceptanceGatesNewSupplyAndPublicChanges(t *testing.T) {
	f := setup(t)
	private := f.channel(t, "private")
	public := f.channel(t, "public")
	mux := http.NewServeMux()
	f.s.Register(mux, func(*http.Request) (channelmarket.Actor, error) {
		return channelmarket.Actor{UserID: 1}, nil
	})
	call := func(method, path, body string) int {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		return w.Code
	}
	if got := call("POST", "/api/marketplace/channels", `{}`); got != 428 {
		t.Fatalf("new supply without acceptance: %d", got)
	}
	if got := call("PATCH", "/api/marketplace/channels/"+private.ID, `{"visibility":"public"}`); got != 428 {
		t.Fatalf("publication without acceptance: %d", got)
	}
	if got := call("PATCH", "/api/marketplace/channels/"+private.ID, `{"visibility":"private"}`); got != 200 {
		t.Fatalf("existing private channel management blocked: %d", got)
	}
	if got := call("PATCH", "/api/marketplace/channels/"+public.ID, `{"visibility":"public"}`); got != 200 {
		t.Fatalf("existing public channel management blocked: %d", got)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_identity.policy_acceptances(user_id,document,version,locale) VALUES(1,'supplier',$1,'zh-HK')`, identity.CurrentPolicyVersion); err != nil {
		t.Fatal(err)
	}
	if got := call("PATCH", "/api/marketplace/channels/"+private.ID, `{"visibility":"public"}`); got != 200 {
		t.Fatalf("accepted publication failed: %d", got)
	}
	if got := call("POST", "/api/marketplace/channels", `{}`); got == 428 {
		t.Fatal("accepted owner still gated")
	}
}
