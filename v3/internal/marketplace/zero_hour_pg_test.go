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
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func zeroHourUsage(f *fixture, user int64, request string, amount credits.Micro) error {
	return pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		return f.s.RecordUsageTx(testContext, tx, user, request, amount)
	})
}

func TestZeroHourSettledUsageReplayBoundaryAndRollback(t *testing.T) {
	f := newFixture(t)
	for _, amount := range []credits.Micro{999999, 1, 1000000} {
		if err := zeroHourUsage(f, 1, fmt.Sprint(int64(amount)), amount); err != nil {
			t.Fatal(err)
		}
	}
	if err := zeroHourUsage(f, 1, "999999", 999999); err != nil {
		t.Fatal(err)
	}
	if err := zeroHourUsage(f, 1, "999999", 999998); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	assertZeroState(t, f, 1, 2, 2000000, 0)
	failure := errors.New("settlement failure")
	err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		if err := f.s.RecordUsageTx(testContext, tx, 1, "rollback", 1000000); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	assertZeroState(t, f, 1, 2, 2000000, 0)
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.operations WHERE request_id='rollback'`); n != 0 {
		t.Fatalf("rolled-back usage persisted %d operations", n)
	}
	if err := zeroHourUsage(f, 1, "cap", 1000000000); err != nil {
		t.Fatal(err)
	}
	assertZeroState(t, f, 1, 1000, 1002000000, 0)
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_zero_hour_states SET usage_micro=$1 WHERE user_id=1`, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if err := zeroHourUsage(f, 1, "overflow", 1); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow: %v", err)
	}
	if err := zeroHourUsage(f, 1, "negative", -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative usage: %v", err)
	}
}

func TestZeroHourConcurrentUsageCountsFractionsOnce(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	failed := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failed <- zeroHourUsage(f, 1, fmt.Sprintf("part:%d", i/2), 100000)
		}()
	}
	wg.Wait()
	close(failed)
	for err := range failed {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertZeroState(t, f, 1, 1, 1000000, 0)
}

func TestZeroHourHiddenDrawBeforePaidProgressAndRollsBack(t *testing.T) {
	f := newFixture(t)
	if err := zeroHourUsage(f, 1, "seed", 1000000); err != nil {
		t.Fatal(err)
	}
	f.s.cfg.Draw = func(limit int64) (int64, error) {
		if limit != zeroHourDrawLimit {
			return 0, errors.New("wrong draw range")
		}
		return 1049, nil
	}
	err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, hit, err := f.s.tryZeroHourTx(testContext, tx, 1, true)
		if err != nil {
			return err
		}
		if hit {
			return errors.New("draw exactly at threshold must miss")
		}
		return f.s.advancePaidZeroHourTx(testContext, tx, 1, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	assertZeroState(t, f, 1, 6, 1000000, 0)
	f.s.cfg.Draw = func(int64) (int64, error) { return 0, nil }
	failure := errors.New("reward insertion failed")
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		reward, hit, err := f.s.tryZeroHourTx(testContext, tx, 1, false)
		if err != nil {
			return err
		}
		if !hit || reward.PropType != zeroHourPropType || reward.DurationSeconds != 3600 || reward.MultiplierPPM != 0 {
			return fmt.Errorf("hidden reward: %+v hit=%v", reward, hit)
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	assertZeroState(t, f, 1, 6, 1000000, 0)
	if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, hit, err := f.s.tryZeroHourTx(testContext, tx, 1, false)
		if err != nil {
			return err
		}
		if !hit {
			return errors.New("free standard order can win")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertZeroState(t, f, 1, 0, 0, 1)
	for _, draw := range []int64{-1, zeroHourDrawLimit} {
		f.s.cfg.Draw = func(int64) (int64, error) { return draw, nil }
		err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			_, _, err := f.s.tryZeroHourTx(testContext, tx, 1, true)
			return err
		})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("out of range draw %d: %v", draw, err)
		}
	}
}

func TestZeroHourExistingPropSuppressesHiddenRewardAndOverview(t *testing.T) {
	f := newFixture(t)
	var recordID, propID int64
	if err := f.pool.QueryRow(testContext, `INSERT INTO v3_marketplace.blind_box_open_records(user_id,request_id,reward) VALUES(1,'zero-hour-test','{}') RETURNING id`).Scan(&recordID); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(testContext, `INSERT INTO v3_marketplace.blind_box_props(user_id,open_record_id,kind,title,multiplier_ppm,duration_seconds,prop_type) VALUES(1,$1,'multiplier','zero',0,3600,$2) RETURNING id`, recordID, zeroHourPropType).Scan(&propID); err != nil {
		t.Fatal(err)
	}
	f.s.cfg.Draw = func(int64) (int64, error) { return 0, errors.New("blocked card must not draw") }
	for _, status := range []string{"available", "paused", "active"} {
		until := time.Unix(f.now.Load(), 0).Add(time.Hour)
		if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_props SET status=$2,expires_at=$3 WHERE id=$1`, propID, status, until); err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			_, hit, err := f.s.tryZeroHourTx(testContext, tx, 1, true)
			if hit {
				return errors.New("existing card must suppress hidden hit")
			}
			return err
		}); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		view, err := f.s.ZeroHourOverview(testContext, 1)
		if err != nil || view.Active != (status == "active") || (view.Active && view.ActiveUntil != until.Unix()) {
			t.Fatalf("%s overview: %+v %v", status, view, err)
		}
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_props SET expires_at=$2 WHERE id=$1`, propID, time.Unix(f.now.Load(), 0)); err != nil {
		t.Fatal(err)
	}
	f.s.cfg.Draw = func(int64) (int64, error) { return 999, nil }
	if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, hit, err := f.s.tryZeroHourTx(testContext, tx, 1, true)
		if err != nil {
			return err
		}
		if !hit {
			return errors.New("card expired exactly now must permit hit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	view, err := f.s.ZeroHourOverview(testContext, 1)
	if err != nil || view.Active || view.Points != 0 || view.CurrentProbability != .0001 || view.MaxProbability != .005 {
		t.Fatalf("expired overview: %+v %v", view, err)
	}
}

func assertZeroState(t *testing.T, f *fixture, user, points, usage, hits int64) {
	t.Helper()
	var gotPoints, gotUsage, gotHits int64
	if err := f.pool.QueryRow(testContext, `SELECT points,usage_micro,hit_count FROM v3_marketplace.blind_box_zero_hour_states WHERE user_id=$1`, user).Scan(&gotPoints, &gotUsage, &gotHits); err != nil {
		t.Fatal(err)
	}
	if gotPoints != points || gotUsage != usage || gotHits != hits {
		t.Fatalf("zero-hour state points/usage/hits %d/%d/%d, want %d/%d/%d", gotPoints, gotUsage, gotHits, points, usage, hits)
	}
}
