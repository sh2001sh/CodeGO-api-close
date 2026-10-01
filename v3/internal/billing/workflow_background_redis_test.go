//go:build pgintegration

package billing

import (
	"bytes"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
)

func TestBackgroundRestartRefreshAndSweepUsesEncryptedJobFacts(t *testing.T) {
	s, rdb, l, c := setup(t, 1000)
	s.loader = &asyncLoader{loader: l, exists: false}
	key := bytes.Repeat([]byte{7}, 32)
	repository, err := live.NewRedisBackgroundRepository(rdb, "billing-background", key)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewBackgroundSettler(s, repository)
	request := newReq("resp_bg_restart")
	request.Targets = []gateway.Target{{ChannelID: 3, CredentialID: 300, Secret: "secret"}, {ChannelID: 4, CredentialID: 400}}
	data, err := adapter.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	job := live.BackgroundJob{ID: request.ID, UserID: 7, KeyID: 70, Group: "default", Model: "gpt", Body: request.Body,
		ChannelID: 3, CredentialID: 300, Reservation: data, Status: "in_progress", CreatedAt: c.now()}
	if err := repository.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	restartedRepository, err := live.NewRedisBackgroundRepository(rdb, "billing-background", key)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewBackgroundSettler(s, restartedRepository)
	persisted, err := restartedRepository.GetOwned(ctx, job.ID, 7, 70)
	if err != nil {
		t.Fatal(err)
	}
	restored := newReq(job.ID) // live Refresh intentionally runs before target resolution
	if err := restarted.Refresh(ctx, restored, persisted.Reservation); err != nil {
		t.Fatal(err)
	}
	c.ms.Add((48 * time.Hour).Milliseconds())
	if n, err := s.SweepExpired(ctx, 100); n != 0 || err != nil {
		t.Fatalf("active background release=%d err=%v", n, err)
	}
	if _, held := balance(t, rdb); held != 208 {
		t.Fatalf("running background held=%d", held)
	}
	foreign := newReq(job.ID)
	foreign.Principal.UserID++
	if err := restarted.Refresh(ctx, foreign, data); err == nil {
		t.Fatal("foreign user restored background hold")
	}
	out := completed(10, 120)
	for range 2 {
		if err := restarted.Finalize(ctx, restored, data, out); err != nil {
			t.Fatal(err)
		}
	}
	if bal, held := balance(t, rdb); bal != 750 || held != 0 || len(events(t, rdb)) != 1 {
		t.Fatalf("money=%d/%d events=%d", bal, held, len(events(t, rdb)))
	}
	if err := restarted.Refresh(ctx, restored, data); err != nil {
		t.Fatalf("completed refresh=%v", err)
	}
}

func TestBackgroundVerifiedOrphanReleaseAndUnverifiedRetention(t *testing.T) {
	for _, withFacts := range []bool{false, true} {
		t.Run(map[bool]string{false: "facts unavailable", true: "verified absent"}[withFacts], func(t *testing.T) {
			s, rdb, _, c := setup(t, 1000)
			adapter := NewBackgroundSettler(s)
			if withFacts {
				repo, err := live.NewRedisBackgroundRepository(rdb, "orphan-facts", bytes.Repeat([]byte{7}, 32))
				if err != nil {
					t.Fatal(err)
				}
				adapter = NewBackgroundSettler(s, repo)
			}
			request := newReq("resp_bg_orphan")
			request.Targets = []gateway.Target{{ChannelID: 3, CredentialID: 300}}
			data, err := adapter.Reserve(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			c.ms.Add((48 * time.Hour).Milliseconds())
			n, err := s.SweepExpired(ctx, 100)
			_, held := balance(t, rdb)
			if withFacts {
				if err != nil || n != 1 || held != 0 {
					t.Fatalf("orphan release=%d held=%d error=%v", n, held, err)
				}
			} else {
				if err == nil || n != 0 || held != 208 {
					t.Fatalf("unknown release=%d held=%d error=%v", n, held, err)
				}
				if err := adapter.Finalize(ctx, request, data, gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
