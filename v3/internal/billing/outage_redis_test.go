//go:build pgintegration

package billing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// cutProxy forwards TCP to Redis until cut, then drops every connection and
// refuses new ones until restored: a Redis outage as the gateway sees it.
type cutProxy struct {
	ln       net.Listener
	upstream string
	mu       sync.Mutex
	down     bool
	conns    map[net.Conn]struct{}
}

func newCutProxy(t *testing.T, upstream string) *cutProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &cutProxy{ln: ln, upstream: upstream, conns: map[net.Conn]struct{}{}}
	t.Cleanup(func() { _ = ln.Close(); p.cut() })
	go p.serve()
	return p
}

func (p *cutProxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		down := p.down
		p.mu.Unlock()
		if down {
			_ = c.Close()
			continue
		}
		up, err := net.Dial("tcp", p.upstream)
		if err != nil {
			_ = c.Close()
			continue
		}
		p.track(c, up)
		go func() { _, _ = io.Copy(up, c); _ = up.Close() }()
		go func() { _, _ = io.Copy(c, up); _ = c.Close() }()
	}
}

func (p *cutProxy) track(cs ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range cs {
		p.conns[c] = struct{}{}
	}
}

func (p *cutProxy) cut() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down = true
	for c := range p.conns {
		_ = c.Close()
	}
	p.conns = map[net.Conn]struct{}{}
}

func (p *cutProxy) restore() {
	p.mu.Lock()
	p.down = false
	p.mu.Unlock()
}

// userAccounts maps user n to account n, so tests can use several accounts.
type userAccounts struct{}

func (userAccounts) WalletAccount(_ context.Context, user int64) (int64, error) { return user, nil }

func outageSetup(t *testing.T, balance int64) (*Settler, *redisx.Client, *cutProxy) {
	t.Helper()
	addr := os.Getenv("V3_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_REDIS_ADDR not set")
	}
	direct, err := redisx.Connect(redisx.Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = direct.Close() })
	if err := direct.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	proxy := newCutProxy(t, addr)
	viaProxy, err := redisx.Connect(redisx.Config{Addr: proxy.ln.Addr().String(), PoolSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = viaProxy.Close() })
	s, err := New(viaProxy, func() *catalog.Snapshot { return snap }, userAccounts{}, &loader{balance: balance},
		Config{WALDir: t.TempDir(), RedisTimeout: 100 * time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, direct, proxy
}

func reqFor(user int64, id string) *gateway.Request {
	r := newReq(id)
	r.Principal.UserID = user
	return r
}

func hotBalance(t *testing.T, rdb *redisx.Client, acct int64) (bal, reserved int64) {
	t.Helper()
	v := rdb.HMGet(ctx, BalanceKey(acct), "balance", "reserved").Val()
	bal, _ = strconv.ParseInt(v[0].(string), 10, 64)
	reserved, _ = strconv.ParseInt(v[1].(string), 10, 64)
	return bal, reserved
}

// Acceptance (tasks.md M2): through a Redis outage, known accounts keep
// working within their allowance, and after recovery Redis holds exactly
// the charges made during the outage, each once.
func TestOutageAllowanceWALAndReplay(t *testing.T) {
	s, direct, proxy := outageSetup(t, 1_000_000)
	warm := reqFor(7, "warm")
	if err := s.Reserve(ctx, warm); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, warm, completed(10, 20)); err != nil { // 50
		t.Fatal(err)
	}
	held := reqFor(7, "held") // reserved in Redis, finalized during the outage
	if err := s.Reserve(ctx, held); err != nil {
		t.Fatal(err)
	}

	proxy.cut()
	if err := s.Finalize(ctx, held, completed(10, 20)); err != nil {
		t.Fatalf("finalize during outage: %v", err)
	}
	if err := s.Reserve(ctx, reqFor(8, "stranger")); !errors.Is(err, gateway.ErrBillingUnavailable) {
		t.Fatalf("account never seen by this gateway = %v; want refused", err)
	}
	admitted := 0
	for i := 0; ; i++ {
		req := reqFor(7, "out"+strconv.Itoa(i))
		err := s.Reserve(ctx, req)
		if errors.Is(err, gateway.ErrBillingUnavailable) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Finalize(ctx, req, completed(10, 20)); err != nil {
			t.Fatal(err)
		}
		admitted++
	}
	// Allowance is 10% of the last seen balance (999,950): 99,995. Each
	// request holds 208 then settles at 50; "held" already spent 50.
	if spent := int64(50 * (admitted + 1)); admitted < 1000 || spent > 99_995 {
		t.Fatalf("admitted %d outage requests (spent %d); want many, within the 99,995 allowance", admitted, spent)
	}
	_, _, _, _, pending := s.Stats()
	// The failed stranger admission also records one zero-charge cancellation
	// to fence a reserve command whose response may have been lost.
	if pending != int64(admitted+2) {
		t.Fatalf("wal pending = %d; want %d", pending, admitted+2)
	}

	proxy.restore()
	time.Sleep(1100 * time.Millisecond) // breaker cooldown
	if err := s.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	want := int64(1_000_000 - 50*(admitted+2))
	if bal, res := hotBalance(t, direct, 7); bal != want || res != 0 {
		t.Fatalf("after replay balance=%d reserved=%d; want %d/0", bal, res, want)
	}
	if n := direct.XLen(ctx, redisx.StreamBillingEvents).Val(); n != int64(admitted+3) {
		t.Fatalf("stream entries = %d; want %d (every charge and cancellation exactly once)", n, admitted+3)
	}
	cancellations := 0
	for _, event := range events(t, direct) {
		if event[FieldRequestID] == "stranger" {
			cancellations++
			if event[FieldAmount] != "0" || event[FieldModel] != "" {
				t.Fatal("refused admission emitted billable usage")
			}
		}
	}
	if cancellations != 1 {
		t.Fatalf("cancellations=%d", cancellations)
	}
	if _, _, _, replayed, pending := s.Stats(); replayed != int64(admitted+2) || pending != 0 {
		t.Fatalf("replayed=%d pending=%d", replayed, pending)
	}
	if err := s.Reserve(ctx, reqFor(7, "after")); err != nil {
		t.Fatalf("normal path did not resume after replay: %v", err)
	}
}

// A replay interrupted and restarted (crash after applying part of a
// segment) must not charge anything twice.
func TestReplayIsIdempotent(t *testing.T) {
	s, direct, proxy := outageSetup(t, 1_000_000)
	warm := reqFor(7, "warm")
	if err := s.Reserve(ctx, warm); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, warm, completed(10, 20)); err != nil {
		t.Fatal(err)
	}
	proxy.cut()
	for i := 0; i < 20; i++ {
		req := reqFor(7, "o"+strconv.Itoa(i))
		if err := s.Reserve(ctx, req); err != nil {
			t.Fatal(err)
		}
		if err := s.Finalize(ctx, req, completed(10, 20)); err != nil {
			t.Fatal(err)
		}
	}
	segs, err := s.wal.sealed()
	if err != nil || len(segs) != 1 {
		t.Fatalf("segments = %v, %v", segs, err)
	}
	copyPath := segs[0].path + ".copy"
	data, _ := os.ReadFile(segs[0].path)
	if err := os.WriteFile(copyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	proxy.restore()
	time.Sleep(1100 * time.Millisecond)
	if err := s.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	first, _ := hotBalance(t, direct, 7)
	if err := os.Rename(copyPath, segs[0].path); err != nil { // the "crashed" segment reappears
		t.Fatal(err)
	}
	s.wal.pending.Add(20)
	if err := s.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	if second, _ := hotBalance(t, direct, 7); second != first || first != 1_000_000-50*21 {
		t.Fatalf("balance after first replay %d, after second %d; want both %d", first, second, 1_000_000-50*21)
	}
	if n := direct.XLen(ctx, redisx.StreamBillingEvents).Val(); n != 21 {
		t.Fatalf("stream entries = %d; want 21", n)
	}
}

// Without a WAL directory an outage fails fast instead of queueing.
func TestOutageModeDisabledFailsFast(t *testing.T) {
	s, _, proxy := outageSetup(t, 1_000_000)
	s.wal.close()
	s.wal = nil
	warm := reqFor(7, "warm")
	if err := s.Reserve(ctx, warm); err != nil {
		t.Fatal(err)
	}
	proxy.cut()
	start := time.Now()
	err := s.Reserve(ctx, reqFor(7, "x"))
	if err == nil || errors.Is(err, gateway.ErrInsufficientCredits) || time.Since(start) > time.Second {
		t.Fatalf("reserve without WAL during outage = %v after %v; want a fast error", err, time.Since(start))
	}
	_ = credits.Micro(0)
}
