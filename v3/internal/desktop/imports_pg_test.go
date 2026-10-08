//go:build pgintegration

package desktop

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func TestDesktopImportsAreOwnedEncryptedAndAtomicallyConsumed(t *testing.T) {
	s, a, b := desktopFixture(t)
	ctx := context.Background()
	k, raw, err := s.id.CreateKey(ctx, a.ID, identity.KeyInput{Name: "config"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateImport(ctx, b.ID, ImportInput{Tool: "codex", TokenID: k.ID}); !errors.Is(err, ErrMissing) {
		t.Fatal("other account import", err)
	}
	created, err := s.CreateImport(ctx, a.ID, ImportInput{Tool: "codex", TokenID: k.ID})
	if err != nil {
		t.Fatal(err)
	}
	code := created["code"].(string)
	var stored []byte
	if err := s.pool.QueryRow(ctx, `SELECT ciphertext FROM v3_identity.desktop_imports WHERE code_hash=$1`, digest(code)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) == raw {
		t.Fatal("plaintext stored")
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			out, err := s.ConsumeImport(ctx, code)
			if err == nil && out.APIKey != raw {
				t.Error("wrong key")
			}
			results <- err
		})
	}
	wg.Wait()
	close(results)
	wins, missing := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrMissing) {
			missing++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || missing != 1 {
		t.Fatalf("wins %d missing %d", wins, missing)
	}
	created, err = s.CreateImport(ctx, a.ID, ImportInput{Tool: "claude", TokenID: k.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.id.DeleteKey(ctx, a.ID, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeImport(ctx, created["code"].(string)); !errors.Is(err, ErrMissing) {
		t.Fatal("deleted key consumed", err)
	}
}
