//go:build pgintegration

package marketplace

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func monthlyGrant(f *fixture, user, duration int64, reference string) error {
	return pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		return f.s.GrantMonthlyCardTx(testContext, tx, user, duration, reference)
	})
}

func monthlySeed(t *testing.T, f *fixture, status string, duration, remaining int64, expiry *time.Time, reference string) int64 {
	t.Helper()
	var id int64
	err := f.pool.QueryRow(testContext, `INSERT INTO v3_marketplace.blind_box_props(user_id,kind,title,status,multiplier_ppm,duration_seconds,remaining_seconds,prop_type,expires_at,benefit_reference) VALUES(1,'multiplier','monthly',$1,100000,$2,$3,$4,$5,$6) RETURNING id`, status, duration, remaining, monthlyCardType, expiry, reference).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func monthlyRead(t *testing.T, f *fixture, id int64) monthlyCard {
	t.Helper()
	var card monthlyCard
	err := f.pool.QueryRow(testContext, `SELECT id,duration_seconds,remaining_seconds,status,benefit_reference,expires_at FROM v3_marketplace.blind_box_props WHERE id=$1`, id).Scan(&card.id, &card.duration, &card.remaining, &card.status, &card.reference, &card.expiry)
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func TestMonthlyCardNewReplayAndConflict(t *testing.T) {
	f := newFixture(t)
	if err := monthlyGrant(f, 1, 60, "payment:first"); err != nil {
		t.Fatal(err)
	}
	card := monthlyRead(t, f, 1)
	if card.status != "available" || card.remaining != 60 || card.duration != 60 || card.expiry != nil || card.reference != "payment:first" {
		t.Fatalf("new card: %+v", card)
	}
	for range 3 {
		if err := monthlyGrant(f, 1, 60, "payment:first"); err != nil {
			t.Fatal(err)
		}
	}
	if err := monthlyGrant(f, 1, 61, "payment:first"); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	if got := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_props`); got != 1 {
		t.Fatalf("card count %d", got)
	}
	if _, err := f.s.UseProp(testContext, 1, 1); err != nil {
		t.Fatal(err)
	}
	f.now.Add(61)
	if err := monthlyGrant(f, 1, 60, "payment:first"); err != nil {
		t.Fatal(err)
	}
	if err := monthlyGrant(f, 1, 30, "payment:second"); err != nil {
		t.Fatal(err)
	}
	if monthlyRead(t, f, 1).status != "expired" || monthlyRead(t, f, 2).remaining != 30 {
		t.Fatal("expired card replay revived or changed new grant")
	}
	var factor int64
	if err := f.pool.QueryRow(testContext, `SELECT multiplier_ppm FROM v3_marketplace.account_profiles WHERE user_id=1`).Scan(&factor); err != nil || factor != 1000000 {
		t.Fatalf("expired profile factor=%d err=%v", factor, err)
	}
}

func TestMonthlyCardMergeStatesAndActiveProfile(t *testing.T) {
	for _, status := range []string{"available", "paused", "active", "expired"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t)
			now := f.s.cfg.Now()
			expiry := now.Add(90*time.Second + 250*time.Millisecond)
			if status == "expired" {
				expiry = now
			}
			var expires *time.Time
			if status == "active" || status == "expired" {
				expires = &expiry
			}
			storedStatus := status
			if status == "expired" {
				storedStatus = "active"
			}
			id := monthlySeed(t, f, storedStatus, 120, 500, expires, "old")
			if err := monthlyGrant(f, 1, 60, "payment:next"); err != nil {
				t.Fatal(err)
			}
			card := monthlyRead(t, f, id)
			switch status {
			case "active":
				if card.status != "active" || card.remaining != 151 || card.expiry == nil || !card.expiry.Equal(expiry.Add(time.Minute)) {
					t.Fatalf("stale active duration used: %+v", card)
				}
				var factor int64
				var projected time.Time
				if err := f.pool.QueryRow(testContext, `SELECT multiplier_ppm,expires_at FROM v3_marketplace.account_profiles WHERE user_id=1`).Scan(&factor, &projected); err != nil || factor != 100000 || !projected.Equal(*card.expiry) {
					t.Fatalf("profile factor=%d expiry=%v err=%v", factor, projected, err)
				}
			case "expired":
				if card.status != "expired" || monthlyRead(t, f, id+1).status != "available" {
					t.Fatalf("expiry boundary %+v", card)
				}
			default:
				if card.status != status || card.remaining != 560 || card.duration != 120 || card.expiry != nil || card.reference != "old|payment:next" {
					t.Fatalf("merged card %+v", card)
				}
			}
		})
	}
}

func TestMonthlyCardConcurrentGrants(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- monthlyGrant(f, 1, 10, fmt.Sprintf("payment:%d", i%8))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := monthlyRead(t, f, 1).remaining; got != 80 {
		t.Fatalf("concurrent duration %d", got)
	}
	if got := f.count(t, `SELECT count(*) FROM v3_marketplace.operations WHERE kind='monthly_card'`); got != 8 {
		t.Fatalf("receipt count %d", got)
	}
}

func TestMonthlyCardRollbackAndLimits(t *testing.T) {
	f := newFixture(t)
	injected := errors.New("source failed")
	err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		if err := f.s.GrantMonthlyCardTx(testContext, tx, 1, 60, "source:rollback"); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_props`) != 0 || f.count(t, `SELECT count(*) FROM v3_marketplace.operations`) != 0 || f.count(t, `SELECT count(*) FROM v3_marketplace.account_profiles`) != 0 {
		t.Fatalf("rollback failed %v", err)
	}
	if err := monthlyGrant(f, 1, 60, "source:rollback"); err != nil {
		t.Fatal(err)
	}
	for _, duration := range []int64{-1, 0, 366*86400 + 1, math.MaxInt64} {
		if err := monthlyGrant(f, 1, duration, "invalid"); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("duration %d: %v", duration, err)
		}
	}
	if err := monthlyGrant(f, 1, 1, "  "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank reference: %v", err)
	}
	if err := monthlyGrant(f, 99, 1, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	if err := f.s.GrantMonthlyCardTx(testContext, nil, 1, 1, "nil"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil tx: %v", err)
	}
	if err := monthlyGrant(f, 2, 366*86400, "maximum"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_props SET remaining_seconds=$1 WHERE user_id=1`, math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	if err := monthlyGrant(f, 1, 1, "overflow"); !errors.Is(err, ErrConflict) {
		t.Fatalf("aggregate overflow: %v", err)
	}
	if got := f.count(t, `SELECT count(*) FROM v3_marketplace.operations WHERE user_id=1`); got != 1 {
		t.Fatalf("invalid grant receipt count %d", got)
	}
}

func TestMonthlyCardRetainedMergeAndReceipt(t *testing.T) {
	f := newFixture(t)
	id := monthlySeed(t, f, "paused", 30, 0, nil, "old:source")
	other := monthlySeed(t, f, "available", 40, 40, nil, "another")
	if err := monthlyGrant(f, 1, 60, "new:source"); err != nil {
		t.Fatal(err)
	}
	if card := monthlyRead(t, f, id); card.status != "paused" || card.remaining != 130 {
		t.Fatalf("retained fallback/merge %+v", card)
	}
	if card := monthlyRead(t, f, other); card.status != "used" || card.remaining != 0 {
		t.Fatalf("retained secondary %+v", card)
	}
	if err := monthlyGrant(f, 1, 40, "another"); err != nil {
		t.Fatal(err)
	}
	if monthlyRead(t, f, id).remaining != 130 {
		t.Fatal("historical source duplicated")
	}
	if err := monthlyGrant(f, 1, 41, "another"); !errors.Is(err, ErrConflict) {
		t.Fatalf("retained source fingerprint %v", err)
	}
}

func TestMonthlyCardOpaqueReferences(t *testing.T) {
	f := newFixture(t)
	for _, reference := range []string{"receipt|part", "receipt", " part ", "part"} {
		if err := monthlyGrant(f, 1, 10, reference); err != nil {
			t.Fatal(err)
		}
	}
	if got := monthlyRead(t, f, 1).remaining; got != 40 {
		t.Fatalf("opaque reference mistaken for substring replay: %d", got)
	}
	if err := monthlyGrant(f, 1, 10, "receipt|part"); err != nil {
		t.Fatal(err)
	}
	if got := monthlyRead(t, f, 1).remaining; got != 40 {
		t.Fatalf("opaque replay duplicate: %d", got)
	}
}
