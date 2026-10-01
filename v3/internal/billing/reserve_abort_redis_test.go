//go:build pgintegration

package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func TestRejectedReserveDurablyReleasesLostReplyAndFencesLateCommand(t *testing.T) {
	for _, mode := range []string{"wallet", "funding", "source"} {
		for _, execute := range []bool{false, true} {
			for _, canceled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/committed=%t/canceled=%t", mode, execute, canceled), func(t *testing.T) {
					s, rdb, req, want := abortFixture(t, mode)
					callCtx, cancel := context.WithCancel(context.WithValue(ctx, abortRequestKey{}, true))
					defer cancel()
					hook := &lostReserveReply{execute: execute}
					if canceled {
						hook.cancel = cancel
					}
					rdb.AddHook(hook)
					if err := s.Reserve(callCtx, req); !errors.Is(err, gateway.ErrBillingUnavailable) {
						t.Fatalf("admission=%v", err)
					}
					if req.Reserve != nil || hook.calls.Load() != 1 {
						t.Fatalf("rejected request admitted or not exercised: reserve=%v calls=%d", req.Reserve, hook.calls.Load())
					}
					if s.wal.pending.Load() != 1 {
						t.Fatalf("durable cancellations=%d, want 1", s.wal.pending.Load())
					}
					if err := s.Replay(ctx); err != nil {
						t.Fatal(err)
					}
					for id, expected := range want {
						values, err := rdb.HMGet(ctx, BalanceKey(id), "balance", "reserved", "ver").Result()
						if err != nil || fmt.Sprint(values[0]) != fmt.Sprint(expected) || values[1] != "0" || values[2] != "1" {
							t.Fatalf("account %d after cancellation=%v err=%v", id, values, err)
						}
					}
					if n := rdb.ZCard(ctx, redisx.KeyReservationOpen).Val(); n != 0 {
						t.Fatalf("open holds=%d", n)
					}
					if mode == "source" {
						for key, value := range rdb.HGetAll(ctx, BalanceKey(43)).Val() {
							if strings.HasSuffix(key, ":reserved") && value != "0" {
								t.Fatalf("model hold %s=%s", key, value)
							}
						}
					}
					before := len(events(t, rdb))
					for _, event := range events(t, rdb) {
						if event[FieldAmount] != "0" || event[FieldModel] != "" {
							t.Fatalf("rejected request emitted financial usage: %v", event)
						}
					}
					if err := s.Replay(ctx); err != nil || len(events(t, rdb)) != before {
						t.Fatalf("duplicate cancellation changed stream: %v", err)
					}
					if err := s.Reserve(ctx, req); err == nil {
						t.Fatal("late reserve ignored the durable cancellation fence")
					}
				})
			}
		}
	}
}

func TestRejectedReserveReportsWALFailure(t *testing.T) {
	s, rdb, req, _ := abortFixture(t, "wallet")
	s.wal.close()
	rdb.AddHook(&lostReserveReply{execute: true})
	err := s.Reserve(context.WithValue(ctx, abortRequestKey{}, true), req)
	if !errors.Is(err, gateway.ErrBillingUnavailable) || !strings.Contains(err.Error(), "cancel") {
		t.Fatalf("durable cancellation failure not reported: %v", err)
	}
}

func TestRejectedReserveWithoutWALCancelsUsingDetachedContext(t *testing.T) {
	for _, mode := range []string{"wallet", "funding", "source"} {
		t.Run(mode, func(t *testing.T) {
			s, rdb, req, want := abortFixture(t, mode)
			s.wal.close()
			s.wal = nil
			callCtx, cancel := context.WithCancel(context.WithValue(ctx, abortRequestKey{}, true))
			defer cancel()
			rdb.AddHook(&lostReserveReply{execute: true, cancel: cancel})
			if err := s.Reserve(callCtx, req); !errors.Is(err, gateway.ErrBillingUnavailable) {
				t.Fatalf("admission=%v", err)
			}
			for id, expected := range want {
				values, err := rdb.HMGet(ctx, BalanceKey(id), "balance", "reserved").Result()
				if err != nil || fmt.Sprint(values[0]) != fmt.Sprint(expected) || values[1] != "0" {
					t.Fatalf("account %d=%v err=%v", id, values, err)
				}
			}
		})
	}
}

func TestAcceptedOptionalOutageFallbackRetainsItsSettlement(t *testing.T) {
	s, rdb, req, _ := abortFixture(t, "wallet")
	s.cfg.DisableOutageAdmission = false
	s.local = newLocalBalances(.9)
	s.local.observe(account, 1000)
	rdb.AddHook(&lostReserveReply{execute: true})
	if err := s.Reserve(context.WithValue(ctx, abortRequestKey{}, true), req); err != nil {
		t.Fatal(err)
	}
	if !req.Reserve.(*hold).local || s.wal.pending.Load() != 0 {
		t.Fatal("accepted local work was prematurely canceled")
	}
	if err := s.Finalize(ctx, req, completed(10, 20)); err != nil {
		t.Fatal(err)
	}
	if err := s.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	if b, h := balance(t, rdb); b != 950 || h != 0 {
		t.Fatalf("accepted settlement=%d/%d", b, h)
	}
	if len(events(t, rdb)) != 1 {
		t.Fatal("accepted settlement was lost or duplicated")
	}
}
