//go:build pgintegration

package channelmarket_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestGroupFavoritesOwnershipVisibilityIdempotencyAndPagination(t *testing.T) {
	f := setup(t)
	public := f.channel(t, "public")
	f.active(t, public)
	private := f.channel(t, "private")
	f.active(t, private)
	consumer := channelmarket.Actor{UserID: 2}
	owner := channelmarket.Actor{UserID: 1}
	if err := f.s.SaveGroupFavorite(ctx, consumer, private.GroupID, true); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("private group leaked: %v", err)
	}
	if err := f.s.SaveGroupFavorite(ctx, consumer, "missing", true); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("unknown group: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f.s.SaveGroupFavorite(ctx, consumer, public.GroupID, true); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	page, err := f.s.ListGroupFavorites(ctx, consumer, 1, 24, nil)
	if err != nil || page.Pagination.Total != 1 || len(page.Items) != 1 || !page.Items[0].Available || page.Items[0].ID != public.ID || page.Items[0].Name == "" {
		t.Fatalf("favorite=%+v err=%v", page, err)
	}
	created := page.Items[0].CreatedAt
	if err = f.s.SaveGroupFavorite(ctx, consumer, public.GroupID, true); err != nil {
		t.Fatal(err)
	}
	page, err = f.s.ListGroupFavorites(ctx, consumer, 1, 24, nil)
	if err != nil || !page.Items[0].CreatedAt.Equal(created) {
		t.Fatal("duplicate add moved bookmark timestamp", err)
	}
	other, err := f.s.ListGroupFavorites(ctx, owner, 1, 24, nil)
	if err != nil || other.Pagination.Total != 0 {
		t.Fatal("other account saw favorite", err)
	}
	if err = f.s.SaveGroupFavorite(ctx, owner, public.GroupID, false); err != nil {
		t.Fatal(err)
	}
	page, err = f.s.ListGroupFavorites(ctx, consumer, 1, 24, nil)
	if err != nil || page.Pagination.Total != 1 {
		t.Fatal("other account removed favorite", err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.group_access(group_id,user_id) VALUES($1,2)`, private.GroupID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.SaveGroupFavorite(ctx, consumer, private.GroupID, true); err != nil {
		t.Fatal(err)
	}
	filtered, err := f.s.ListGroupFavorites(ctx, consumer, 1, 100, []string{public.GroupID})
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].GroupID != public.GroupID {
		t.Fatalf("filtered=%+v err=%v", filtered, err)
	}
	page, err = f.s.ListGroupFavorites(ctx, consumer, 1, 1, nil)
	if err != nil || page.Pagination.Total != 2 || len(page.Items) != 1 {
		t.Fatal("pagination failed", err, page)
	}
	if _, err = f.pool.Exec(ctx, `DELETE FROM v3_channelmarket.group_access WHERE group_id=$1 AND user_id=2`, private.GroupID); err != nil {
		t.Fatal(err)
	}
	page, err = f.s.ListGroupFavorites(ctx, consumer, 1, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.GroupID == private.GroupID && (item.Available || item.Name != "" || item.PublicSlug != "" || len(item.Models) > 0 || item.Multiplier != nil) {
			t.Fatalf("revoked metadata leaked: %+v", item)
		}
	}
	if err = f.s.SetBlock(ctx, owner, public.InternalChannelID, 2, true); err != nil {
		t.Fatal(err)
	}
	page, err = f.s.ListGroupFavorites(ctx, consumer, 1, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.Available {
			t.Fatalf("blocked/revoked group still accessible: %+v", item)
		}
	}
	if err = f.s.SaveGroupFavorite(ctx, consumer, public.GroupID, true); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatal("blocked add allowed", err)
	}
	for _, id := range []string{public.GroupID, private.GroupID, "missing"} {
		if err = f.s.SaveGroupFavorite(ctx, consumer, id, false); err != nil {
			t.Fatal(err)
		}
	}
	page, err = f.s.ListGroupFavorites(ctx, consumer, 1, 24, nil)
	if err != nil || page.Pagination.Total != 0 {
		t.Fatal("cannot remove unavailable bookmarks", err)
	}
	mux := http.NewServeMux()
	f.s.Register(mux, func(*http.Request) (channelmarket.Actor, error) { return consumer, nil })
	for _, body := range []string{`{"group_id":"missing"}`, `{"group_id":"","favorite":true}`, `{"group_id":"missing","favorite":null}`, `{"group_id":"missing","favorite":true,"user_id":1}`} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/marketplace/group-favorites", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("payload=%s status=%d body=%s", body, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("PUT", "/api/marketplace/group-favorites", strings.NewReader(`{"group_id":"missing","favorite":false}`))
	r.Header.Set("Origin", "https://other.example")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-site mutation accepted", w.Code)
	}
	anonymous := http.NewServeMux()
	f.s.Register(anonymous, func(*http.Request) (channelmarket.Actor, error) {
		return channelmarket.Actor{}, errors.New("no session")
	})
	w = httptest.NewRecorder()
	anonymous.ServeHTTP(w, httptest.NewRequest("GET", "/api/marketplace/group-favorites", nil))
	if w.Code != 401 {
		t.Fatal("anonymous favorites allowed", w.Code)
	}
}
