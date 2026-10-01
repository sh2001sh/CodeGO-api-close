//go:build pgintegration

package marketplace

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestOpenLimitsStandardSiteWideConcurrentLastSlot(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	p.Scope, p.Standard.Enabled, p.DailyOpenLimit = "standard", true, 1
	if _, err := f.s.SavePool(testContext, p); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_identity.users SET role='admin' WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	for user := int64(1); user <= 2; user++ {
		if _, err := f.s.GrantBoxes(testContext, 3, user, fmt.Sprintf("global-grant:%d", user), p.ID, 1); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for user := int64(1); user <= 2; user++ {
		wg.Add(1)
		go func(user int64) {
			defer wg.Done()
			<-start
			_, err := f.s.OpenBoxes(testContext, user, fmt.Sprintf("global-open:%d", user), 1)
			results <- err
		}(user)
	}
	close(start)
	wg.Wait()
	close(results)
	var admitted, refused int
	for err := range results {
		switch {
		case err == nil:
			admitted++
		case errors.Is(err, ErrOpenLimit):
			refused++
		default:
			t.Fatalf("unexpected cross-user error: %v", err)
		}
	}
	if admitted != 1 || refused != 1 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records`) != 1 ||
		f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`) != 1 {
		t.Fatalf("site-wide final slot overshot: admitted=%d refused=%d", admitted, refused)
	}
	f.now.Add(int64((24 * time.Hour).Seconds()))
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_open_records(user_id,pool_id,created_at,request_id,reward)
 VALUES(3,$1,$2,'different-user-grant','{}')`, p.ID, f.s.cfg.Now()); err != nil {
		t.Fatal(err)
	}
	for user := int64(1); user <= 2; user++ {
		if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE owner_user_id=$1 AND status='available'`, user) == 0 {
			continue
		}
		if _, err := f.s.OpenBoxes(testContext, user, "blocked-by-other-user-grant", 1); !errors.Is(err, ErrOpenLimit) {
			t.Fatalf("another user's grant open did not consume site-wide cap: %v", err)
		}
	}
}
