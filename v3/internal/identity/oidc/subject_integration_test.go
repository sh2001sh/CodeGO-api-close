//go:build pgintegration

package oidc

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func testEnsureSubject(t *testing.T, f *fixture) {
	t.Helper()
	ctx := context.Background()
	if _, err := New(f.pool, Config{}, nil, nil); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected disabled OIDC: %v", err)
	}
	if sub, err := EnsureSubject(ctx, f.pool, 1); err != nil || sub != "ABC234" {
		t.Fatalf("existing subject changed with OIDC disabled: %q %v", sub, err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES (4,'community_concurrent')`); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		sub string
		err error
	}
	results := make(chan outcome, 12)
	var wg sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub, err := EnsureSubject(ctx, f.pool, 4)
			results <- outcome{sub, err}
		}()
	}
	wg.Wait()
	close(results)
	var subject string
	for result := range results {
		if result.err != nil || len(result.sub) != 6 || subject != "" && subject != result.sub {
			t.Fatalf("concurrent subject not stable: previous=%q actual=%q error=%v", subject, result.sub, result.err)
		}
		subject = result.sub
	}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_identity.users SET status='disabled' WHERE id=4`); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSubject(ctx, f.pool, 4); !errors.Is(err, ErrInactive) {
		t.Fatalf("disabled user accepted: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_identity.users SET status='active',deleted_at=now() WHERE id=4`); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSubject(ctx, f.pool, 4); !errors.Is(err, ErrInactive) {
		t.Fatalf("deleted user accepted: %v", err)
	}
	if _, err := EnsureSubject(ctx, f.pool, 999999); !errors.Is(err, ErrInactive) {
		t.Fatalf("missing user accepted: %v", err)
	}
}
