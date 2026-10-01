package live

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type attachmentsRepository struct {
	mu                 sync.Mutex
	items              map[string]Locator
	getError, putError error
	gets               int
}

func attachmentsRepositoryKey(id string, user, key int64) string {
	return fmt.Sprintf("%s:%d:%d", id, user, key)
}

func (r *attachmentsRepository) Put(_ context.Context, item Locator, ttl time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.putError != nil {
		return r.putError
	}
	if ttl <= 0 {
		return errors.New("invalid TTL")
	}
	if r.items == nil {
		r.items = make(map[string]Locator)
	}
	r.items[attachmentsRepositoryKey(item.ID, item.UserID, item.KeyID)] = item
	return nil
}

func (r *attachmentsRepository) Get(_ context.Context, id string, user, key int64) (Locator, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gets++
	if r.getError != nil {
		return Locator{}, r.getError
	}
	item, ok := r.items[attachmentsRepositoryKey(id, user, key)]
	if !ok {
		return Locator{}, ErrNotFound
	}
	return item, nil
}

func (r *attachmentsRepository) Delete(_ context.Context, id string, user, key int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, attachmentsRepositoryKey(id, user, key))
	return nil
}

func attachmentsHandler(t *testing.T, store FileStore, repo Repository) *Handler {
	t.Helper()
	t.Setenv("FILE_DELIVERY_BASE_URL", "")
	return &Handler{cfg: Config{Files: store, Repository: repo, Client: &http.Client{}, MaxBodyBytes: 1 << 20, LocatorTTL: 24 * time.Hour}}
}

func attachmentsRequest(id string, protocol gateway.Protocol) *gateway.Request {
	return &gateway.Request{Protocol: protocol, Model: "m", Principal: gateway.Principal{UserID: 11, KeyID: 101}, Body: []byte(fmt.Sprintf(`{"model":"m","input":[{"type":"input_file","file_id":%q}]}`, id))}
}

func attachmentsTarget(base string) gateway.Target {
	return gateway.Target{Provider: "openai", BaseURL: base, Secret: "test-upstream-secret", ChannelID: 7, CredentialID: 9}
}
