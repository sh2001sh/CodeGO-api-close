package live

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type backgroundAuth struct{}

func (backgroundAuth) Authorize(_ context.Context, key string) (gateway.Principal, error) {
	switch key {
	case "owner":
		return gateway.Principal{UserID: 1, KeyID: 11}, nil
	case "other-key":
		return gateway.Principal{UserID: 1, KeyID: 12}, nil
	case "other-user":
		return gateway.Principal{UserID: 2, KeyID: 11}, nil
	case "unavailable":
		return gateway.Principal{}, gateway.ErrAuthUnavailable
	default:
		return gateway.Principal{}, errors.New("bad key")
	}
}

type backgroundPlanner struct{}

func (backgroundPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	panic("background requests must use the saved route")
}

func (backgroundPlanner) Report(gateway.Target, gateway.AttemptResult) {}

type backgroundSettler struct{}

func (backgroundSettler) Reserve(context.Context, *gateway.Request) error {
	panic("background lookup must not reserve twice")
}

func (backgroundSettler) Finalize(context.Context, *gateway.Request, gateway.Outcome) error {
	panic("background lookup must not finalize twice")
}

type backgroundRepository struct {
	locator Locator
	err     error
	unsafe  bool
}

func (repo *backgroundRepository) Put(_ context.Context, item Locator, _ time.Duration) error {
	repo.locator = item
	return nil
}

func (repo *backgroundRepository) Get(_ context.Context, id string, user, key int64) (Locator, error) {
	if repo.err != nil {
		return Locator{}, repo.err
	}
	if !repo.unsafe && (repo.locator.ID != id || repo.locator.UserID != user || repo.locator.KeyID != key) {
		return Locator{}, ErrNotFound
	}
	return repo.locator, nil
}

func (*backgroundRepository) Delete(context.Context, string, int64, int64) error { return nil }

type backgroundLimits struct {
	acquires int
	releases int
	err      error
}

func (limits *backgroundLimits) Acquire(context.Context, *gateway.Request, gateway.Target) error {
	limits.acquires++
	return limits.err
}

func (limits *backgroundLimits) Release(context.Context, *gateway.Request, gateway.Target) error {
	limits.releases++
	return nil
}

func backgroundFixture(t *testing.T, target gateway.Target) (*Handler, *http.ServeMux, *backgroundRepository, *backgroundLimits) {
	t.Helper()
	target.ChannelID, target.CredentialID = 7, 9
	if target.Provider == "" {
		target.Provider = "openai"
	}
	if target.Secret == "" {
		target.Secret = "upstream-secret"
	}
	repo := &backgroundRepository{locator: Locator{ID: "resp_123", UserID: 1, KeyID: 11, ChannelID: 7, CredentialID: 9, Model: "gpt-test"}}
	limits := &backgroundLimits{}
	h, err := New(Config{Auth: backgroundAuth{}, Planner: backgroundPlanner{}, Settler: backgroundSettler{},
		Repository: repo, Limits: limits, Resolve: func(_ context.Context, channel, credential int64) (gateway.Target, error) {
			if channel != 7 || credential != 9 {
				t.Errorf("resolver received unexpected route %d/%d", channel, credential)
			}
			return target, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.registerBackground(mux)
	return h, mux, repo, limits
}

func backgroundCall(mux *http.ServeMux, method, path, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

type backgroundReadError struct{ io.Reader }

func (body backgroundReadError) Read(p []byte) (int, error) {
	if body.Reader != nil {
		return body.Reader.Read(p)
	}
	return 0, io.ErrUnexpectedEOF
}
