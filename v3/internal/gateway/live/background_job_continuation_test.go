package live

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type backgroundContinuationPlanner []gateway.Target

func (targets backgroundContinuationPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return append([]gateway.Target(nil), targets...), nil
}

func (backgroundContinuationPlanner) Report(gateway.Target, gateway.AttemptResult) {}

func TestBackgroundJobsNativeContinuationRejectsForeignOwnerBeforeReservation(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer up.Close()
	for _, owner := range []struct{ user, key int64 }{{2, 11}, {1, 12}} {
		h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
		// Even a repository that returns a foreign locator must not authorize it.
		h.cfg.Repository = &backgroundRepository{unsafe: true, locator: Locator{
			ID: "resp_native_previous", UserID: owner.user, KeyID: owner.key, ChannelID: 7, CredentialID: 9,
		}}
		r := httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"gpt-test","background":true,"previous_response_id":"resp_native_previous"}`))
		r.Header.Set("Authorization", "Bearer owner")
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, r)
		if err := h.Reconcile(context.Background(), 10); err != nil {
			t.Fatal(err)
		}
		pending, err := repo.Pending(context.Background(), 10)
		if err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusBadRequest || billing.reserves != 0 || len(pending) != 0 || calls.Load() != 0 {
			t.Fatalf("foreign continuation status=%d reserves=%d jobs=%d upstream=%d", w.Code, billing.reserves, len(pending), calls.Load())
		}
	}
}

func TestBackgroundJobsNativeContinuationPinsChannelAndCredential(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if gjson.GetBytes(body, "previous_response_id").Str != "resp_native_previous" {
			t.Errorf("native continuation changed: %s", body)
		}
		_, _ = io.WriteString(w, backgroundCompletedSnapshot)
	}))
	defer up.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	target, _ := h.cfg.Resolve(context.Background(), 7, 9)
	otherChannel, otherCredential := target, target
	otherChannel.ChannelID = 8
	otherCredential.CredentialID = 10
	h.cfg.Planner = backgroundContinuationPlanner{otherChannel, otherCredential, target}
	h.cfg.Repository = &backgroundRepository{locator: Locator{
		ID: "resp_native_previous", UserID: 1, KeyID: 11, ChannelID: 7, CredentialID: 9,
	}}
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true,"previous_response_id":"resp_native_previous"}`)
	job, err := repo.GetOwned(context.Background(), id, 1, 11)
	if err != nil {
		t.Fatal(err)
	}
	if job.ChannelID != 7 || job.CredentialID != 9 || billing.reserves != 1 {
		t.Fatalf("continuation was not pinned: channel=%d credential=%d reserves=%d", job.ChannelID, job.CredentialID, billing.reserves)
	}
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	out, count, _ := billing.result(id)
	if calls.Load() != 1 || !out.Charge || count != 1 {
		t.Fatalf("owned continuation upstream=%d outcome=%+v count=%d", calls.Load(), out, count)
	}
}

func TestBackgroundJobsNativeContinuationUnavailableRouteCannotUseOtherChannel(t *testing.T) {
	h, _, wrapped, _, billing := backgroundJobsFixture(t, "http://unused.invalid", "openai")
	h.cfg.Repository = &backgroundRepository{locator: Locator{
		ID: "resp_native_previous", UserID: 1, KeyID: 11, ChannelID: 8, CredentialID: 9,
	}}
	r := httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"gpt-test","background":true,"previous_response_id":"resp_native_previous"}`))
	r.Header.Set("Authorization", "Bearer owner")
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || billing.reserves != 0 {
		t.Fatalf("unavailable original route status=%d reserves=%d", w.Code, billing.reserves)
	}
}
