//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestMarketReverificationKeepsPublishedChannelAndSeparatesProbeSamples(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	crypto, err := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := channelmarket.New(f.pool, crypto, f.poster, channelmarket.Config{Now: func() time.Time { return time.Unix(f.now.Load(), 0) }, Probe: func(context.Context, channelmarket.ProbeRequest) (channelmarket.ModelTest, error) {
		return channelmarket.ModelTest{}, errors.New("fixture upstream failed")
	}}, nil)
	if _, err = s.QueueVerification(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	published, err := s.Get(ctx, channelmarket.Actor{}, c.ID)
	if err != nil || published.Status != "active" || published.Verification != "passed" {
		t.Fatalf("retest withdrew published channel: %+v %v", published, err)
	}
	if n, err := s.ProcessVerification(ctx, 1); err != nil || n != 1 {
		t.Fatalf("retest: %d %v", n, err)
	}
	published, err = s.Get(ctx, channelmarket.Actor{}, c.ID)
	if err != nil || published.Status != "active" || published.Verification != "passed" {
		t.Fatalf("failed retest changed publication: %+v %v", published, err)
	}
	insight, err := s.ModelInsights(ctx, channelmarket.Actor{}, c.ID, "fixture-model", 24)
	if err != nil || insight.RequestCount != 0 || insight.IndependentConsumers != 0 || insight.SuccessRate != nil || insight.ProbeCount != 2 || insight.ProbeSuccessCount != 1 || insight.ProbeSuccessRate == nil || *insight.ProbeSuccessRate != .5 {
		t.Fatalf("probe samples polluted real traffic or disappeared: %+v %v", insight, err)
	}
	slot := published.RecentRequests[len(published.RecentRequests)-1]
	if slot.RequestCount != 0 || slot.ProbeCount != 2 || slot.ProbeSuccessRate != 50 {
		t.Fatalf("recent probes: %+v", slot)
	}
	if _, err = s.QueueVerification(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if err = s.PauseVerification(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	published, err = s.Get(ctx, channelmarket.Actor{}, c.ID)
	if err != nil || published.Status != "active" || published.Verification != "passed" {
		t.Fatalf("pausing retest changed approved channel: %+v %v", published, err)
	}
}

func TestDisablingAutoProbeCancelsQueuedAndInFlightChecks(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	owner := channelmarket.Actor{UserID: 1}
	update := func(enabled bool) {
		t.Helper()
		patch, _ := json.Marshal(map[string]bool{"auto_probe_enabled": enabled})
		if _, err := f.s.Update(ctx, owner, c.InternalChannelID, patch); err != nil {
			t.Fatal(err)
		}
	}
	update(true)
	if n, err := f.s.QueueAutoProbes(ctx, 1); err != nil || n != 1 {
		t.Fatalf("queue: %d %v", n, err)
	}
	update(false)
	if n, err := f.s.ProcessVerification(ctx, 1); err != nil || n != 0 {
		t.Fatalf("disabled queued probe ran: %d %v", n, err)
	}
	update(true)
	// The canceled queue entry still enforces the configured interval.
	f.now.Add(3601)
	if n, err := f.s.QueueAutoProbes(ctx, 1); err != nil || n != 1 {
		t.Fatalf("requeue: %d %v", n, err)
	}
	crypto, err := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var calls atomic.Int32
	s := channelmarket.New(f.pool, crypto, f.poster, channelmarket.Config{Now: func() time.Time { return time.Unix(f.now.Load(), 0) }, Probe: func(ctx context.Context, _ channelmarket.ProbeRequest) (channelmarket.ModelTest, error) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		return channelmarket.ModelTest{}, ctx.Err()
	}}, nil)
	finished := make(chan error, 1)
	go func() { _, err := s.ProcessVerification(ctx, 1); finished <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not start")
	}
	update(false)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disabling auto-probe left upstream request running")
	}
	if n, err := s.QueueAutoProbes(ctx, 1); err != nil || n != 0 || calls.Load() != 1 {
		t.Fatalf("disabled probe restarted: %d %v calls=%d", n, err, calls.Load())
	}
	var status string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM v3_channelmarket.verification_runs WHERE channel_id=$1 AND trigger='auto_probe' ORDER BY created_at DESC LIMIT 1`, c.InternalChannelID).Scan(&status); err != nil || status != "paused" {
		t.Fatalf("canceled probe result accepted: %s %v", status, err)
	}
}

func TestMissingOrExpiredCredentialCompletesVerificationAsFailed(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	if _, err := f.pool.Exec(ctx, `UPDATE v3_catalog.channel_credentials SET expires_at=$2 WHERE channel_id=$1`, c.InternalChannelID, time.Unix(f.now.Load(), 0).Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.QueueVerification(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.ProcessVerification(ctx, 1); err != nil || n != 1 {
		t.Fatalf("expired credential left test queued: %d %v", n, err)
	}
	got, err := f.s.Get(ctx, channelmarket.Actor{UserID: 1}, c.ID)
	if err != nil || got.Verification != "failed" || got.CompletedAt == nil || len(got.ModelResults) != 1 || got.ModelResults[0].Status != "failed" {
		t.Fatalf("missing terminal failure: %+v %v", got, err)
	}
}
