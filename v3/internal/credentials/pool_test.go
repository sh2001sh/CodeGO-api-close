package credentials

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type refreshFunc func(context.Context, Credential) (Credential, error)

func (f refreshFunc) Refresh(ctx context.Context, c Credential) (Credential, error) { return f(ctx, c) }

type memoryStore struct {
	mu    sync.Mutex
	creds []Credential
}

func (s *memoryStore) RunExclusive(ctx context.Context, run func(context.Context) error) error {
	return run(ctx)
}
func (s *memoryStore) List(context.Context) ([]Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Credential(nil), s.creds...), nil
}
func (s *memoryStore) Refresh(ctx context.Context, c Credential, r Refresher) (Credential, error) {
	fresh, err := r.Refresh(ctx, c)
	if err != nil {
		return Credential{}, err
	}
	fresh.UpdatedAt = time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, old := range s.creds {
		if old.ID == c.ID {
			s.creds[i] = fresh
		}
	}
	return fresh, nil
}

func TestThousandSimultaneousExpirationsRespectProviderQPS(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		store := &memoryStore{}
		for id := int64(1); id <= 1000; id++ {
			store.creds = append(store.creds, Credential{ID: id, Provider: "codex", ExpiresAt: time.Now()})
		}
		var starts []time.Time
		var ids []int64
		refresher := refreshFunc(func(_ context.Context, c Credential) (Credential, error) {
			starts = append(starts, time.Now())
			ids = append(ids, c.ID)
			c.ExpiresAt = time.Now().Add(time.Hour)
			if len(starts) == 1000 {
				cancel()
			}
			return c, nil
		})
		pool, err := New(store, map[string]Refresher{"codex": refresher}, Config{QPS: 5, ReloadEvery: time.Hour}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		if err = pool.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if len(starts) != 1000 {
			t.Fatalf("refreshes=%d, want 1000", len(starts))
		}
		for i := 1; i < len(starts); i++ {
			if starts[i].Sub(starts[i-1]) < 200*time.Millisecond {
				t.Fatalf("refresh %d exceeds 5 QPS", i)
			}
			if ids[i] <= ids[i-1] {
				t.Fatalf("expiry heap order: %v before %v", ids[i-1], ids[i])
			}
		}
		t.Logf("1000 simultaneous expirations: refresh interval=%v, elapsed=%v, limit=5 QPS", starts[1].Sub(starts[0]), starts[999].Sub(starts[0]))
	})
}

func TestProviderCircuitPausesFailuresWithoutBlockingAnotherProvider(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		store := &memoryStore{}
		for id := int64(1); id <= 10; id++ {
			store.creds = append(store.creds, Credential{ID: id, Provider: "broken", ExpiresAt: time.Now()})
		}
		store.creds = append(store.creds, Credential{ID: 100, Provider: "healthy", ExpiresAt: time.Now()})
		var starts []time.Time
		var healthyAt time.Time
		broken := refreshFunc(func(context.Context, Credential) (Credential, error) {
			starts = append(starts, time.Now())
			if len(starts) == 4 {
				cancel()
			}
			return Credential{}, errors.New("endpoint failed")
		})
		healthy := refreshFunc(func(_ context.Context, c Credential) (Credential, error) {
			healthyAt = time.Now()
			c.ExpiresAt = time.Now().Add(time.Hour)
			return c, nil
		})
		pool, err := New(store, map[string]Refresher{"broken": broken, "healthy": healthy}, Config{QPS: 100, ReloadEvery: time.Hour}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		if err = pool.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if len(starts) != 4 || starts[3].Sub(starts[2]) < time.Minute {
			t.Fatalf("circuit did not pause: %v", starts)
		}
		if healthyAt.IsZero() || !healthyAt.Before(starts[3]) {
			t.Fatal("healthy provider blocked by failed provider")
		}
	})
}

func TestReloadRemovesDisabledCredentialsAndPreservesRetryDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		store := &memoryStore{creds: []Credential{{ID: 1, Provider: "p", ExpiresAt: time.Now()}}}
		calls := 0
		r := refreshFunc(func(context.Context, Credential) (Credential, error) {
			calls++
			return Credential{}, errors.New("unavailable")
		})
		pool, err := New(store, map[string]Refresher{"p": r}, Config{ReloadEvery: time.Second, RetryBackoff: 10 * time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- pool.Run(ctx) }()
		synctest.Wait()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if calls != 1 {
			t.Fatalf("reload bypassed retry deadline: calls=%d", calls)
		}
		store.mu.Lock()
		store.creds = nil
		store.mu.Unlock()
		time.Sleep(12 * time.Second)
		synctest.Wait()
		cancel()
		if err = <-done; err != nil {
			t.Fatal(err)
		}
		if calls != 1 {
			t.Fatalf("disabled credential refreshed: calls=%d", calls)
		}
	})
}

func TestProviderAliasesShareOneRefreshRateLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		store := &memoryStore{creds: []Credential{
			{ID: 1, Provider: "anthropic", ExpiresAt: time.Now()},
			{ID: 2, Provider: "claude", ExpiresAt: time.Now()},
		}}
		var starts []time.Time
		r := refreshFunc(func(_ context.Context, c Credential) (Credential, error) {
			starts = append(starts, time.Now())
			c.ExpiresAt = time.Now().Add(time.Hour)
			if len(starts) == 2 {
				cancel()
			}
			return c, nil
		})
		pool, err := New(store, map[string]Refresher{"claude": r}, Config{ProviderQPS: map[string]int{"claude": 5}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		if err = pool.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if len(starts) != 2 || starts[1].Sub(starts[0]) < 200*time.Millisecond {
			t.Fatalf("alias bypassed shared QPS: %v", starts)
		}
	})
}
