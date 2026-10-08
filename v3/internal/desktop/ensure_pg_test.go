//go:build pgintegration

package desktop

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDesktopEnsureConcurrentReplayKeepsOneSecretAndDeniesInvalidGroup(t *testing.T) {
	s, a, _ := desktopFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	type result struct {
		id      int64
		key     string
		created bool
		err     error
	}
	results := make(chan result, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			k, key, created, err := s.ensureKey(ctx, a.ID, "Code Go Desktop - Concurrent", "")
			results <- result{k.ID, key, created, err}
		})
	}
	wg.Wait()
	close(results)
	var id int64
	var key string
	created := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if id == 0 {
			id, key = r.id, r.key
		}
		if r.id != id || r.key != key {
			t.Fatal("concurrent ensure returned different keys")
		}
		if r.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created=%d", created)
	}
	if _, _, _, err := s.ensureKey(ctx, a.ID, "Code Go Desktop - Concurrent", "unauthorized-group"); !errors.Is(err, ErrDenied) {
		t.Fatal("existing key bypassed requested group validation", err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.api_keys WHERE user_id=$1 AND name='Code Go Desktop - Concurrent'`, a.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}
