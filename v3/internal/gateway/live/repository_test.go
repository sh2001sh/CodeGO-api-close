package live

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisRepositoryRejectsInvalidLocator(t *testing.T) {
	if _, err := NewRedisRepository(nil, ""); err == nil {
		t.Fatal("nil Redis accepted")
	}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer func() { _ = client.Close() }()
	r, err := NewRedisRepository(client, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []Locator{{}, {ID: "x", UserID: 1, KeyID: 1, ChannelID: 1}, {ID: "x", UserID: 1, ChannelID: 1, CredentialID: 1}} {
		if err := r.Put(context.Background(), item, time.Hour); err == nil {
			t.Fatal("invalid locator accepted")
		}
	}
	if r.key("x:{bad}", 1, 2) == r.key("x:{bad}", 1, 3) || r.key("x:{bad}", 1, 2) == r.key("other", 1, 2) {
		t.Fatal("locator keys collide")
	}
}

func TestRedisRepositoryPersistenceOwnerIsolationAndExpiry(t *testing.T) {
	addr := os.Getenv("CODEGO_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("CODEGO_TEST_REDIS_ADDR not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer func() { _ = client.Close() }()
	prefix := "live-test:" + requestID() + ":"
	repo, err := NewRedisRepository(client, prefix)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	item := Locator{ID: "resp_test", UserID: 1, KeyID: 2, ChannelID: 3, CredentialID: 4, Model: "model", CreatedAt: time.Now().UTC()}
	if err := repo.Put(ctx, item, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewRedisRepository(client, prefix)
	loaded, err := restarted.Get(ctx, item.ID, 1, 2)
	if err != nil || loaded.ChannelID != 3 || loaded.CredentialID != 4 {
		t.Fatalf("locator=%+v %v", loaded, err)
	}
	for _, owner := range [][2]int64{{2, 2}, {1, 3}, {0, 0}} {
		if _, err := repo.Get(ctx, item.ID, owner[0], owner[1]); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross-owner lookup=%v", err)
		}
	}
	if err := repo.Delete(ctx, item.ID, 1, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, item.ID, 1, 2); err != nil {
		t.Fatalf("other key deleted resource: %v", err)
	}
	if err := repo.Put(ctx, item, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(70 * time.Millisecond)
	if _, err := repo.Get(ctx, item.ID, 1, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired locator=%v", err)
	}
	if err := repo.Put(ctx, item, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, item.ID, 1, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, item.ID, 1, 2); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted locator persisted")
	}
}
