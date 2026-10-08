//go:build pgintegration

package incentives

import (
	"context"
	"sync"
	"testing"
)

func TestMissingAffiliateCodeAllocatedOnceAcrossConcurrentReads(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET aff_code=NULL WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan string, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); code, err := s.AffiliateCode(ctx, 3); codes <- code; errs <- err }()
	}
	wg.Wait()
	close(codes)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	first := ""
	for code := range codes {
		if len(code) != 16 {
			t.Fatalf("bad code %q", code)
		}
		if first == "" {
			first = code
		}
		if first != code {
			t.Fatal("concurrent request changed invitation attribution")
		}
	}
}
